import unittest

import lib
import test_go_package
import test_java_features
import test_proto_py_variant
import test_python2
import test_python_proto
import test_python_suites
import test_ros


class LanguageVariantContractsTest(unittest.TestCase):
    def test_go_dll_accepts_an_already_pic_target(self):
        files = test_go_package.go_world()
        test_go_package.go_module(files, 'app', module='GO_DLL')
        graph = lib.make(files, 'app', '-DPIC=yes')
        shared = lib.node_by_output(graph, '$(B)/app/libapp.so')
        args = test_go_package.go_tool_args(shared)
        self.assertEqual(args[args.index('++mode') + 1], 'dll')

    def test_java_variant_of_plain_go_library_has_a_clear_error(self):
        files = test_go_package.go_world()
        test_go_package.go_module(files, 'app', macros='ENABLE(GO_PROTO)\nINCLUDE_TAGS(JAVA_PROTO)\n')
        result = lib.make_process(files, 'app')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('no Java submodule', result.stderr)
        self.assertNotIn('panic:', result.stderr)

    def test_empty_python2_proto_peer_is_valid(self):
        files = test_python2.python2_base()
        files.update({'app/ya.make': f'PY2_PROGRAM()\n{lib.BARE_MODULE}PEERDIR(p)\nEND()\n',
                      'p/ya.make': f'PROTO_LIBRARY()\n{lib.BARE_MODULE}END()\n'})
        graph = lib.make(files, 'app')
        lib.node_by_output(graph, '$(B)/app/app')
        self.assertFalse(any('__intpy3__' in output for node in graph['graph'] for output in node['outputs']))

    def test_python3_program_checks_binary_and_library_variant_sources(self):
        files = test_python_suites.fixture(
            'IF (MODULE_TAG == \"PY3_BIN_LIB\")\nPY_SRCS(library.py)\n'
            'ELSE()\nPY_SRCS(binary.py)\nENDIF()\n', macro='PY3_PROGRAM')
        files.update({'lib/library.py': '', 'lib/binary.py': ''})
        graph = lib.make(files, 'lib', '-t')
        lib.node_by_output(graph, '$(B)/lib/lib')
        checks = [n for n in graph['graph']
                  if '$(B)/lib/test-results/flake8/meta.json' in n['outputs']]
        self.assertEqual(len(checks), 2)
        self.assertEqual(sorted(test_python_suites.args(n)[-2:] for n in checks), [
            ['$(S)/lib/a.py', '$(S)/lib/binary.py'],
            ['$(S)/lib/a.py', '$(S)/lib/library.py'],
        ])
        test_python_suites.lint(graph, 'import_test')

    def test_python2_extension_used_as_host_tool_receives_python_headers(self):
        files = test_python2.python2_base()
        files.update({'mod/ya.make': f'PY2MODULE(ext)\n{lib.BARE_MODULE}SRCS(ext.c)\nEND()\n',
                      'mod/ext.c': '',
                      'app/ya.make': f'LIBRARY()\n{lib.BARE_MODULE}RUN_PROGRAM(mod OUT result.cpp)\nEND()\n'})
        files['contrib/tools/python/lib/ya.make'] = files['contrib/tools/python/lib/ya.make'].replace('END()', 'CFLAGS(GLOBAL -DHOST_PYTHON_HEADERS)\nEND()')
        lib.tool_program(files, 'tools/fix_elf', 'fix_elf')
        graph = lib.make(files, 'app')
        compiled = lib.node_by_output(graph, '$(B)/mod/ext.c.pic.o')
        self.assertIn('-DHOST_PYTHON_HEADERS', compiled['cmds'][0]['cmd_args'])
        lib.node_by_output(graph, '$(B)/app/result.cpp')

    def test_python_proto_does_not_compile_extracted_gazetteer_source(self):
        files = test_proto_py_variant.py_proto_files()
        files.update({'p/ya.make': 'PROTO_LIBRARY()\nONLY_TAGS(PY3_PROTO)\nSRCS(message.proto)\nFROM_ARCHIVE(input.tar OUT generated.gztproto)\nEND()\n',
                      'p/message.proto': 'syntax = "proto3";\n', 'p/input.tar': '',
                      'q/ya.make': 'PROTO_LIBRARY()\nONLY_TAGS(PY3_PROTO)\nEND()\n'})
        graph = lib.make(files, 'py')
        lib.node_by_output(graph, '$(B)/p/message__intpy3___pb2.py')
        self.assertFalse(any('$(B)/dict/gazetteer/converter/converter' in c['cmd_args']
                             for n in graph['graph'] for c in n['cmds']))

    def test_kapt_ignores_native_processor_classpath_entry(self):
        files = test_java_features.tree('WITH_KOTLIN()\nWITH_KAPT()\nKAPT_ANNOTATION_PROCESSOR_CLASSPATH(native)\nJAVA_SRCS(src/Main.kt)',
                                        **{'native/ya.make': f'LIBRARY()\n{lib.BARE_MODULE}END()\n'})
        jar = lib.node_by_output(lib.make(files, 'app'), '$(B)/app/app.jar')
        self.assertFalse(any('/native/' in p for p in jar['inputs']))
        kapt = test_java_features.command(jar, 'with_kapt_args.py')
        self.assertIn('--ap-classpath', kapt)
        self.assertFalse(any('/native/' in arg for arg in kapt))

    def test_python_ros_without_include_dirs_skips_unresolved_message_types(self):
        files = test_python_proto.python_base()
        files.update(test_ros.ros_files())
        directory = 'contrib/libs/linux-headers/messages'
        files.update({'app/ya.make': f'PY3_PROGRAM()\n{lib.BARE_MODULE}PEERDIR({directory})\nEND()\n',
                      directory + '/ya.make': f'ROS_LIBRARY(robot)\n{lib.BARE_MODULE}ONLY_TAGS(PY3_ROS)\nSET(ROS_PURE_PY yes)\nROS_SRCS(msg/Parent.msg)\nEND()\n',
                      directory + '/msg/Parent.msg': 'Missing value\n',
                      'sdg/sdc/third_party/ros/genpy/yamakes/pylib/ya.make': f'LIBRARY()\n{lib.BARE_MODULE}END()\n'})
        lib.tool_program(files, 'sdg/tier0/tools/generate_proxy_import_file', 'generate_proxy_import_file')
        graph = lib.make(files, 'app')
        parent = lib.node_by_output(graph, f'$(B)/{directory}/msg/_Parent.py')
        self.assertFalse(any('Missing' in p for p in parent['inputs']))


if __name__ == '__main__':
    unittest.main(verbosity=2)
