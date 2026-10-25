import unittest

from tst import lib


class TestDependencyOutputsTest(unittest.TestCase):
    def test_depends_retains_shared_library_and_its_archive_closure(self):
        files = {
            'tests/ya.make': 'EXECTEST()\nRUN(tool)\nDEPENDS(tool shared static)\nEND()\n',
            'shared/ya.make': f'DLL(shared)\n{lib.BARE_MODULE}PEERDIR(native)\nSRCS(shared.cpp)\nEND()\n',
            'shared/shared.cpp': 'int shared;\n',
            'native/ya.make': f'LIBRARY()\n{lib.BARE_MODULE}SRCS(native.cpp)\nEND()\n',
            'native/native.cpp': 'int native;\n',
            'static/ya.make': f'LIBRARY()\n{lib.BARE_MODULE}SRCS(local.cpp GLOBAL global.cpp)\nEND()\n',
            'static/local.cpp': 'int local;\n',
            'static/global.cpp': 'int global;\n',
        }
        lib.tool_program(files, 'tool', 'tool.so')
        for extra in ([], ['--strip-packages-from-results']):
            with self.subTest(extra=extra):
                graph = lib.make(files, 'tests', '-t', '--sandboxing', *extra)
                run = lib.node_by_output(graph, '$(B)/tests/test-results/exectest/meta.json')
                shared = lib.node_by_output(graph, '$(B)/shared/libshared.so')
                archive = lib.node_by_output(graph, '$(B)/native/libnative.a')
                self.assertIn(shared['uid'], run['deps'])
                self.assertIn(archive['uid'], shared['deps'])
                for name in ('libstatic.a', 'libstatic.global.a'):
                    self.assertEqual(lib.node_by_output(graph, '$(B)/static/' + name)['uid'] in run['deps'],
                                     name == 'libstatic.a')
                command = run['cmds'][-1]['cmd_args']
                directories = [command[i + 1] for i, word in enumerate(command) if word == '--dep-root']
                self.assertEqual(directories, ['$(B)/shared', 'tests'])


if __name__ == '__main__':
    unittest.main(verbosity=2)
