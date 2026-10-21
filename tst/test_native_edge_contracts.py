import json
import unittest

import lib
import test_codegen_cuda
import test_fortran
import test_link_conf


def library(body):
    return f'LIBRARY()\n{lib.BARE_MODULE}{body}\nEND()\n'


class NativeEdgeContractsTest(unittest.TestCase):
    def test_named_ci_group_collects_global_archive(self):
        files = {'group/ya.make': 'CI_GROUP(named)\nPEERDIR(native)\nEND()\n',
                 'native/ya.make': library('SRCS(GLOBAL registry.cpp)'), 'native/registry.cpp': ''}
        graph = lib.make(files, 'group')
        group = lib.node_by_output(graph, '$(B)/group/named.ci.pkg.fake')
        archive = lib.node_by_output(graph, '$(B)/native/libnative.global.a')
        self.assertIn(archive['outputs'][0], group['inputs'])
        self.assertIn(archive['uid'], group['deps'])

    def test_dll_exports_argument_reaches_the_linker(self):
        files = test_link_conf.tree(lib.FAKE_LD_CONF)
        files['dll/ya.make'] = f'DLL(test EXPORTS test.exports)\n{lib.BARE_MODULE}SRCS(lib.cpp)\nEND()\n'
        files['dll/test.exports'] = '{};\n'
        graph = lib.make(files, 'dll')
        args = test_link_conf.link(graph, '$(B)/dll/libtest.so')
        self.assertTrue(any('$(S)/dll/test.exports' in arg for arg in args))

    def test_global_linker_inputs_and_library_exclusion(self):
        files = {'app/ya.make': f'PROGRAM()\n{lib.BARE_MODULE}PEERDIR(dep)\nLINK_EXCLUDE_LIBRARIES(dep)\nSRCS(main.cpp)\nEND()\n',
                 'app/main.cpp': '', 'dep/ya.make': library('SRCS(dep.cpp GLOBAL prebuilt.a GLOBAL script.ld GLOBAL rules.supp)'),
                 'dep/dep.cpp': '', 'dep/prebuilt.a': '', 'dep/script.ld': '', 'dep/rules.supp': '',
                 'build/conf/linkers/ld.conf': lib.FAKE_LD_CONF + '\nLINKER_SCRIPT_VALUE=-Wl,--script-marker ${ext=.ld;pre=-T:SRCS_GLOBAL}\n'}
        graph = lib.make(files, 'app')
        args = test_link_conf.link(graph, '$(B)/app/app')
        for value in ('dep/prebuilt.a', '-T$(S)/dep/script.ld', 'dep/rules.supp', '-Wl,--script-marker'):
            self.assertIn(value, args)
        self.assertIn('--exclude-libs', args)
        app = lib.node_by_output(graph, '$(B)/app/app')
        for filename in ('prebuilt.a', 'script.ld', 'rules.supp'):
            self.assertIn('$(S)/dep/' + filename, app['inputs'])

    def test_archive_object_is_linked_without_compilation(self):
        graph = lib.make({'app/ya.make': library('FROM_ARCHIVE(objects.tar OUT object.o)'), 'app/objects.tar': ''}, 'app')
        archive = lib.node_by_output(graph, '$(B)/app/libapp.a')
        unpack = lib.node_by_output(graph, '$(B)/app/object.o')
        self.assertIn(unpack['uid'], archive['deps'])
        self.assertIn('$(B)/app/object.o', archive['inputs'])
        self.assertFalse(any(n['kv']['p'] == 'CC' for n in graph['graph']))

    def test_cuda_invalid_architecture_and_non_split_sanitizer(self):
        files = test_codegen_cuda.CudaTest().split_fixture()
        files['mod/ya.make'] = library('SET(CUDA_ARCHITECTURES invalid)\nCUDA_SRCS(sub/k.cu)')
        result = lib.make_process(files, 'mod')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('CUDA_SRCS: invalid architecture', result.stderr)
        files['build/ymake_conf.py'] = test_codegen_cuda.cuda_conf('CUDA_SANITIZE=yes\n')
        files['mod/ya.make'] = library('SRCS(sub/k.cu)')
        graph = lib.make(files, 'mod')
        compiled = lib.node_by_output(graph, '$(B)/mod/_/sub/k.cu.o')
        self.assertIn('--y_sanitize', compiled['cmds'][0]['cmd_args'])

    def test_incomplete_fortran_include_is_not_a_dependency(self):
        files = test_fortran.world({'mod/ya.make': library('SRCS(main.f)'),
                                   'mod/main.f': "      include ''\n      include 'unfinished\n      end\n"})
        compiled = lib.node_by_output(lib.make(files, 'mod'), '$(B)/mod/main.f.o')
        self.assertEqual(compiled['inputs'], ['$(S)/mod/main.f'])

    def test_unknown_target_architecture_is_diagnosed(self):
        config = json.loads(lib.ya_conf_json())
        tool = config['toolchain']['clang20']
        tool['platforms'].append({'host': {'os': 'LINUX', 'arch': 'x86_64'}, 'target': {'os': 'LINUX', 'arch': 'riscv64'}, 'default': True})
        result = lib.make_process({'app/ya.make': library('SRCS(main.cpp)'), 'app/main.cpp': '',
                                  'build/ya.conf.json': json.dumps(config)},
                                  'app', '--target-platform', 'default-linux-riscv64')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('unsupported platform ISA "riscv64"', result.stderr)
        self.assertNotIn('panic:', result.stderr)

    def test_unit_test_exception_flag_and_afl_peer(self):
        files = {'ut/ya.make': f'UNITTEST()\n{lib.BARE_MODULE}SRCS(test.cpp)\nEND()\n', 'ut/test.cpp': '',
                 'library/cpp/testing/unittest_main/ya.make': library(''),
                 'fuzz/ya.make': f'FUZZ()\n{lib.BARE_MODULE}SRCS(fuzz.cpp)\nEND()\n', 'fuzz/fuzz.cpp': '',
                 'contrib/libs/afl/ya.make': library('CFLAGS(GLOBAL -DAFL_PEER)'),
                 'library/cpp/testing/nofuzz/ya.make': library('')}
        graph = lib.make(files, 'ut', 'fuzz', '-DUT_SKIP_EXCEPTIONS=yes', '-DUSE_AFL=yes')
        self.assertIn('-DUT_SKIP_EXCEPTIONS', lib.node_by_output(graph, '$(B)/ut/test.cpp.o')['cmds'][0]['cmd_args'])
        self.assertIn('-DAFL_PEER', lib.node_by_output(graph, '$(B)/fuzz/fuzz.cpp.o')['cmds'][0]['cmd_args'])


if __name__ == '__main__':
    unittest.main(verbosity=2)
