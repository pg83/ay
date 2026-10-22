import unittest

import lib
from test_java_tests import style_tree, test_nodes


class JavaProjectDependenciesTest(unittest.TestCase):
    def test_style_query_keeps_only_materialized_program_contexts(self):
        files = style_tree(**{
            'app/ya.make': 'JAVA_LIBRARY_NON_SPLIT()\nPEERDIR(program)\nEND()\n',
            'program/ya.make': 'JAVA_PROGRAM()\nWITH_KOTLIN()\nJAVA_SRCS(Main.kt)\nEND()\n',
            'program/Main.kt': 'object Main\n',
            'build/platform/java/scriptgen/ya.make': lib.resource_lib('SCRIPTGEN', 9),
        })
        for requested in (False, True):
            with self.subTest(requested=requested):
                graph = lib.make(files, 'app', *(['program'] if requested else []), '-t',
                                 '--target-platform-flag', 'ADD_PEERDIRS_GEN_TESTS=yes',
                                 '--strip-packages-from-results')
                suite = test_nodes(graph)['program/ktlint']
                expected = {lib.node_by_output(graph, output)['uid'] for output in (
                    '$(B)/common_test.context', '$(B)/program/program.jar',
                    '$(B)/program/all-kt-sources.txt')}
                if requested:
                    expected.add(lib.node_by_output(graph, '$(B)/program/program.run.cp.jar')['uid'])
                else:
                    self.assertFalse(any('$(B)/program/program.run.cp.jar' in node['outputs'] for node in graph['graph']))
                self.assertEqual(set(suite['deps']), expected)


if __name__ == '__main__':
    unittest.main(verbosity=2)
