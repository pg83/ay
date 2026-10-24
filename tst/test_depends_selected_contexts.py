import unittest

import lib


def fixture():
    files = {
        'checks/ya.make': 'EXECTEST()\nRUN(true)\nDEPENDS(dep)\nEND()\n',
        'dep/ya.make': 'LIBRARY()\n' + lib.BARE_MODULE + 'SRCS(local.cpp GLOBAL global.cpp)\nEND()\n',
        'dep/local.cpp': 'int local;\n',
        'dep/global.cpp': 'int global;\n',
        'prior/ya.make': 'DLL(prior)\n' + lib.BARE_MODULE + 'SRCS(main.cpp)\nPEERDIR(dep)\nEND()\n',
        'prior/main.cpp': 'int value;\n',
    }
    lib.tool_program(files, 'tools/fix_elf', 'fix_elf')
    return files


class DependsSelectedContextsTest(unittest.TestCase):
    def check(self, roots, expected):
        graph = lib.make(fixture(), *roots, '-t', '--sandboxing', '--target-platform', 'default-linux-x86_64')
        suite = lib.node_by_output(graph, '$(B)/checks/test-results/exectest/meta.json')
        archives = [n for n in graph['graph'] if n['outputs'] == ['$(B)/dep/libdep.a']]
        self.assertEqual(len(archives), expected)
        self.assertTrue(all(n['uid'] in suite['deps'] for n in archives))
        self.assertEqual({n['inputs'][0] for n in archives},
                         {'$(B)/dep/local.cpp.o'} | ({'$(B)/dep/local.cpp.pic.o'} if expected == 2 else set()))
        for archive in archives:
            compiled = lib.node_by_output(graph, archive['inputs'][0])
            self.assertIn(compiled['uid'], archive['deps'])
            self.assertIn('$(S)/dep/local.cpp', compiled['inputs'])
        self.assertFalse(any(n['uid'] in suite['deps'] for n in graph['graph']
                             if '$(B)/dep/libdep.global.a' in n['outputs']))
        self.assertIn(suite['uid'], graph['result'])
        return graph

    def test_default_library_dependency_is_preserved(self):
        self.check(['checks'], 1)

    def test_all_selected_main_contexts_are_direct_dependencies(self):
        for roots in (['prior', 'checks'], ['checks', 'prior']):
            with self.subTest(roots=roots):
                self.check(roots, 2)

    def test_shared_go_peer_uses_the_native_pic_transition(self):
        from test_go_package import go_world

        files = go_world()
        files.update({
            'checks/ya.make': 'EXECTEST()\nRUN(true)\nDEPENDS(shared pkg)\nEND()\n',
            'shared/ya.make': 'GO_DLL(shared)\nSRCS(main.go)\nEND()\n',
            'shared/main.go': 'package main\n',
            'pkg/ya.make': 'PACKAGE()\nPEERDIR(shared)\nEND()\n',
        })
        graph = lib.make(files, 'checks', '-t', '--sandboxing', '--strip-packages-from-results')
        shared = [n for n in graph['graph'] if n['outputs'][0] == '$(B)/shared/libshared.so']
        self.assertEqual(len(shared), 1)
        suite = lib.node_by_output(graph, '$(B)/checks/test-results/exectest/meta.json')
        self.assertIn(shared[0]['uid'], suite['deps'])
        self.assertEqual(shared[0]['kv']['p'], 'LD')
        self.assertIn('$(S)/shared/main.go', shared[0]['inputs'])
        runtime = '$(B)/contrib/go/_std_1.26/src/runtime/cgo/cgo.a'
        self.assertIn(runtime, shared[0]['inputs'])
        compiled = lib.node_by_output(graph, runtime)
        self.assertIn(compiled['uid'], shared[0]['deps'])


if __name__ == '__main__':
    unittest.main()
