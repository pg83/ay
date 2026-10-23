import unittest

import lib
from test_peer_suites import PEER_TESTS, checks


SCANNER = 'contrib/java/org/sonarsource/scanner/cli/sonar-scanner-cli/2.8'
JAR = 'sonar-scanner-cli-2.8.jar'


class JavaConfigureDependsTest(unittest.TestCase):
    def tree(self, declaration='JTEST_FOR(library)', tag='JTEST_FOR_JAR_COMPILATION'):
        return lib.java_tree(**{
            'root/ya.make': f'PROGRAM()\n{lib.BARE_MODULE}DEPENDS(tests)\nEND()\n',
            'tests/ya.make': f'{declaration}\nONLY_TAGS({tag})\nEND()\n',
            'library/ya.make': 'JAVA_LIBRARY_NON_SPLIT()\nEND()\n',
            'devtools/junit5-runner/ya.make': 'JAVA_LIBRARY_NON_SPLIT()\nEND()\n',
            'devtools/jtest-annotations/junit4/ya.make': 'JAVA_LIBRARY_NON_SPLIT()\nEND()\n',
            'devtools/jtest-annotations/junit5/ya.make': 'JAVA_LIBRARY_NON_SPLIT()\nEND()\n',
            'devtools/junit-runner/ya.make': 'JAVA_LIBRARY_NON_SPLIT()\nEND()\n',
            'build/platform/java/jacoco-agent/ya.make': 'JAVA_CONTRIB()\nEND()\n',
            f'{SCANNER}/ya.make': f'JAVA_CONTRIB()\nVERSION(2.8)\nLOCAL_JAR({JAR})\nEND()\n',
            f'{SCANNER}/{JAR}': '',
        })

    def assert_compilation_context_configures_builtin_depends_without_a_suite(self, declaration, tag):
        graph = lib.make(self.tree(declaration, tag), 'root', '-t', *PEER_TESTS, '--add-modules-to-results')
        scanner = lib.node_by_output(graph, f'$(B)/{SCANNER}/{JAR}')
        self.assertIn(scanner['uid'], graph['result'])
        self.assertEqual(scanner['inputs'], [
            f'$(S)/{SCANNER}/{JAR}', '$(S)/build/scripts/fs_tools.py',
            '$(S)/build/scripts/process_command_files.py',
        ])
        self.assertEqual(scanner['kv']['p'], 'FS')
        self.assertEqual(checks(graph), {})
        compilation = lib.node_by_output(graph, '$(B)/tests/tests.jar')
        self.assertNotIn(scanner['uid'], compilation['deps'])

    def test_compilation_context_configures_builtin_depends_without_a_suite(self):
        for declaration, tag in (('JTEST()', 'JTEST_JAR_COMPILATION'),
                                 ('JTEST_FOR(library)', 'JTEST_FOR_JAR_COMPILATION')):
            with self.subTest(declaration=declaration):
                self.assert_compilation_context_configures_builtin_depends_without_a_suite(declaration, tag)

    def test_default_requires_jtest_compilation_and_depends_traversal(self):
        for declaration, tag, flags in (
            ('JUNIT5()', 'JUNIT5_JAR_COMPILATION', ('-t', *PEER_TESTS)),
            ('JTEST_FOR(library)', 'JTEST_FOR_JAR_COMPILATION', ()),
            ('JTEST_FOR(library)', 'JTEST_FOR_JAR_COMPILATION', ('-t',)),
        ):
            with self.subTest(declaration=declaration, flags=flags):
                graph = lib.make(self.tree(declaration, tag), 'root', *flags, '--add-modules-to-results')
                self.assertFalse(any(output.startswith(f'$(B)/{SCANNER}/')
                                     for node in graph['graph'] for output in node['outputs']))
                self.assertEqual(checks(graph), {})


if __name__ == '__main__':
    unittest.main(verbosity=2)
