import unittest

import lib


CONFIG = '''
when ($MSVC == "yes") {
    C_AS_CXX_FLAGS=/TP /std:c++17
}
otherwise {
    C_AS_CXX_FLAGS=-x c++ -std=c++17
}
'''


def library(body):
    return 'LIBRARY()\nNO_PLATFORM()\nNO_UTIL()\nNO_RUNTIME()\n' + body + 'END()\n'


def fixture(body='', config=CONFIG):
    return {
        'build/ymake.core.conf': lib.CONF_FILES['build/ymake.core.conf'] + config,
        'spruce/leaf/ya.make': library('PEERDIR(spruce/compiler)\n' + body + 'SRCS(plain.c branch.cpp)\n'),
        'spruce/compiler/ya.make': lib.resource_lib('CLANG20', 745),
        'spruce/leaf/plain.c': '#include "leaf.h"\nint plain;\n',
        'spruce/leaf/branch.cpp': '#include "leaf.h"\nint branch;\n',
        'spruce/leaf/leaf.h': '#pragma once\n',
    }


def make(files, *args, targets=('spruce/leaf',)):
    return lib.make(files, *targets, '--target-platform', 'default-linux-x86_64', *args)


def compile_node(graph, source, directory='spruce/leaf'):
    return next(n for n in graph['graph'] if n['kv'].get('p') == 'CC'
                and f'$(S)/{directory}/{source}' in n['inputs'])


class CompileCAsCxxTest(unittest.TestCase):
    def check(self, files, extra, *args):
        graph = make(files, *args)
        archive = lib.node_by_output(graph, '$(B)/spruce/leaf/libspruce-leaf.a')
        self.assertIn(archive['uid'], graph['result'])
        for source, compiler in (('plain.c', 'clang'), ('branch.cpp', 'clang++')):
            node = compile_node(graph, source)
            command = node['cmds'][0]['cmd_args']
            at = command.index('-c')
            self.assertEqual(command[at - len(extra):at], extra)
            self.assertIn('/bin/' + compiler, ' '.join(command[:at]))
            self.assertEqual(command[at:at + 3], ['-c', '-o', node['outputs'][0]])
            self.assertIn('$(S)/spruce/leaf/leaf.h', node['inputs'])
            self.assertIn('$(S)/spruce/leaf/' + source, node['inputs'])
            self.assertIn(node['uid'], archive['deps'])
            self.assertIn(node['outputs'][0], archive['inputs'])
        return graph

    def test_macro_preserves_c_compiler_buckets_and_cpp_standard_order(self):
        flags = 'CFLAGS(-DCOMMON)\nCONLYFLAGS(-DC_ONLY)\nCXXFLAGS(-DCXX_ONLY -std=c++23)\n'
        for body in ('COMPILE_C_AS_CXX()\n' + flags,
                     flags + 'SRCS(early.c)\nCOMPILE_C_AS_CXX()\n'):
            files = fixture(body)
            files['spruce/leaf/early.c'] = '#include "leaf.h"\n'
            graph = self.check(files, ['-x', 'c++', '-std=c++17'])
            for source, standards, included, excluded in (
                ('plain.c', ['-std=c++17'], '-DC_ONLY', '-DCXX_ONLY'),
                ('branch.cpp', ['-std=c++17', '-std=c++20', '-std=c++23'], '-DCXX_ONLY', '-DC_ONLY'),
            ):
                args = compile_node(graph, source)['cmds'][0]['cmd_args']
                self.assertEqual([a for a in args if a.startswith('-std=')], standards)
                self.assertIn('-DCOMMON', args)
                self.assertIn(included, args)
                self.assertNotIn(excluded, args)

    def test_default_c_and_cleared_macro_keep_true_c_and_genuine_cpp(self):
        for body in ('', 'COMPILE_C_AS_CXX()\nSET(EXTRA_C_FLAGS)\n'):
            graph = make(fixture(body))
            c = compile_node(graph, 'plain.c')['cmds'][0]['cmd_args']
            cpp = compile_node(graph, 'branch.cpp')['cmds'][0]['cmd_args']
            self.assertNotIn('-x', c)
            self.assertFalse(any(a.startswith('-std=') for a in c))
            self.assertNotIn('-x', cpp)
            self.assertEqual([a for a in cpp if a.startswith('-std=')], ['-std=c++20'])

    def test_source_order_freezes_macro_value_and_allows_explicit_override(self):
        for body, extra in (
            ('SET(EXTRA_C_FLAGS -x c -std=c11)\nCOMPILE_C_AS_CXX()\n', ['-x', 'c++', '-std=c++17']),
            ('COMPILE_C_AS_CXX()\nSET(EXTRA_C_FLAGS -x c -std=c11)\n', ['-x', 'c', '-std=c11']),
            ('SET(C_AS_CXX_FLAGS -x c++ -std=c++14)\nCOMPILE_C_AS_CXX()\n', ['-x', 'c++', '-std=c++14']),
            ('COMPILE_C_AS_CXX()\nSET(C_AS_CXX_FLAGS -x c++ -std=c++23)\n', ['-x', 'c++', '-std=c++17']),
            ('COMPILE_C_AS_CXX()\nSET_APPEND(EXTRA_C_FLAGS -DEXTRA)\n', ['-x', 'c++', '-std=c++17', '-DEXTRA']),
        ):
            with self.subTest(body=body):
                self.check(fixture(body), extra)

    def test_config_source_template_dependency_and_cli_values(self):
        self.check(fixture('COMPILE_C_AS_CXX()\n', CONFIG.replace('c++17', 'c++14')),
                   ['-x', 'c++', '-std=c++14'])
        self.check(fixture('COMPILE_C_AS_CXX()\n', 'RENAMED_CHOICE=-x c++ -std=c++14\n' +
                           CONFIG.replace('-x c++ -std=c++17', '$RENAMED_CHOICE')),
                   ['-x', 'c++', '-std=c++14'])
        self.check(fixture(), ['-x', 'c++', '-std=c++14'], '-DEXTRA_C_FLAGS=-x c++ -std=c++14')
        self.check(fixture('COMPILE_C_AS_CXX()\n'), ['-x', 'c++', '-std=c++17'],
                   '-DC_AS_CXX_FLAGS=-x c++ -std=c++14')
        graph = self.check(fixture('COMPILE_C_AS_CXX()\n'), ['-x', 'c++', '-std=c++17'],
                           '--target-platform', 'default-linux-aarch64')
        args = compile_node(graph, 'plain.c')['cmds'][0]['cmd_args']
        self.assertIn('--target=aarch64-linux-gnu', args)

    def test_program_vcs_compile_consumes_the_same_extra_c_flags(self):
        files = fixture('COMPILE_C_AS_CXX()\n')
        files['spruce/leaf/ya.make'] = files['spruce/leaf/ya.make'].replace('LIBRARY()', 'PROGRAM(maple)\nALLOCATOR(FAKE)')
        graph = make(files)
        link = lib.node_by_output(graph, '$(B)/spruce/leaf/maple')
        vcs = next(c['cmd_args'] for c in link['cmds']
                   if '$(B)/spruce/leaf/__vcs_version__.c' in c['cmd_args'] and '-c' in c['cmd_args'])
        self.assertEqual(vcs[vcs.index('-c') - 3:vcs.index('-c')], ['-x', 'c++', '-std=c++17'])
        self.assertIn('$(S)/build/scripts/c_templates/svn_interface.c', link['inputs'])
        self.assertIn(link['uid'], graph['result'])

    def test_peers_and_root_order_keep_macro_effect_local(self):
        files = fixture('PEERDIR(spruce/policy)\n')
        files['spruce/policy/ya.make'] = library('COMPILE_C_AS_CXX()\nSRCS(policy.c)\n')
        files['spruce/policy/policy.c'] = 'int policy;\n'
        for targets in (('spruce/leaf', 'spruce/policy'), ('spruce/policy', 'spruce/leaf')):
            graph = make(files, targets=targets)
            self.assertNotIn('-x', compile_node(graph, 'plain.c')['cmds'][0]['cmd_args'])
            args = compile_node(graph, 'policy.c', 'spruce/policy')['cmds'][0]['cmd_args']
            self.assertEqual(args[args.index('-x'):args.index('-x') + 3], ['-x', 'c++', '-std=c++17'])

    def test_host_tool_target_and_generated_input_preserve_real_producers(self):
        files = fixture('IF(ARCH_X86_64)\nCOMPILE_C_AS_CXX()\nENDIF()\n'
                        'ADDINCL(${ARCADIA_BUILD_ROOT}/spruce/leaf)\n'
                        'RUN_PROGRAM(spruce/tool IN seed.txt OUT_NOAUTO generated.h)\n')
        files['spruce/leaf/plain.c'] = '#include "leaf.h"\n#include "generated.h"\nint plain;\n'
        files['spruce/leaf/seed.txt'] = 'generated\n'
        files['spruce/tool/ya.make'] = ('PROGRAM(writer)\nNO_PLATFORM()\nNO_UTIL()\nNO_RUNTIME()\n'
                                       'DISABLE(COMMON_LINK_SETTINGS)\nALLOCATOR(FAKE)\n'
                                       'IF(ARCH_X86_64)\nCOMPILE_C_AS_CXX()\nENDIF()\nSRCS(main.c)\nEND()\n')
        files['spruce/tool/main.c'] = 'int main(){return 0;}\n'
        graph = make(files, '--target-platform', 'default-linux-aarch64')
        target = compile_node(graph, 'plain.c')
        host = compile_node(graph, 'main.c', 'spruce/tool')
        self.assertNotIn('-x', target['cmds'][0]['cmd_args'])
        args = host['cmds'][0]['cmd_args']
        self.assertEqual(args[args.index('-x'):args.index('-x') + 3], ['-x', 'c++', '-std=c++17'])
        generated = lib.node_by_output(graph, '$(B)/spruce/leaf/generated.h')
        tool = lib.node_by_output(graph, '$(B)/spruce/tool/writer')
        self.assertIn(tool['uid'], generated['deps'])
        self.assertIn(tool['outputs'][0], generated['inputs'])
        self.assertIn('$(S)/spruce/leaf/seed.txt', generated['inputs'])
        self.assertIn(generated['uid'], target['deps'])
        self.assertIn(generated['outputs'][0], target['inputs'])
        self.assertIn('$(S)/spruce/leaf/leaf.h', target['inputs'])


if __name__ == '__main__':
    unittest.main()
