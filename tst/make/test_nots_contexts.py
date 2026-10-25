import unittest

from tst.lib import make, node_by_output
from tst.make import test_nots_suites
from tst.make.test_peer_suites import PEER_TESTS, checks


class NotsContextsTest(unittest.TestCase):
    def workspace(self):
        files = test_nots_suites.NotsSuitesTest().workspace(
            'TS_LIBRARY()\nTS_BUILD_SCRIPT(build)\nTS_BUILD_OUTPUTS(build)\nTS_LINT(lint)'
        )
        for peer in ('left', 'right', 'leaf'):
            files[f'{peer}/ya.make'] = 'TS_TSC()\nTS_BUILD_OUTPUTS(build)\nEND()\n'
            files[f'{peer}/index.ts'] = 'export const value = 1;\n'
        files['leaf/ya.make'] += 'RECURSE_FOR_TESTS(tests)\n'
        files['leaf/tests/ya.make'] = 'TS_TEST_FOR(leaf)\nTS_TEST(test)\nEND()\n'
        return files

    def test_self_prepare_peer_discovers_workspace_lint_without_peer_recurses(self):
        for flags, expected in (((), {'web/ts_lint_lint'}), (PEER_TESTS, {
            'web/ts_lint_lint', 'left/eslint', 'right/eslint', 'leaf/eslint',
        })):
            with self.subTest(flags=flags):
                graph = make(self.workspace(), 'web', '--target-platform-tests',
                             '--output-only-tests', '-DAUTOCHECK=yes', *flags)
                suites = checks(graph)
                self.assertEqual(set(suites), expected)
                self.assertEqual(len([n for n in graph['graph'] if n['kv'].get('run_test_node')]), len(expected))
                for peer in ('left', 'right', 'leaf') if flags else ():
                    suite = suites[f'{peer}/eslint']
                    prepare = node_by_output(graph, f'$(B)/{peer}/package.json')
                    self.assertIn(prepare['uid'], suite['deps'])
                    self.assertIn(suite['uid'], graph['result'])

    def test_legacy_test_for_discovers_tested_build_but_modern_selects_preparation(self):
        for macro, expected in ((
            'TS_TEST_JEST_FOR(web)\nTS_TEST_SRCS(../src)',
            {'web/tests/jest', 'web/ts_lint_lint', 'left/eslint', 'right/eslint', 'leaf/eslint'},
        ), (
            'TS_TEST_FOR(web)\nTS_TEST(test)',
            {'web/tests/ts_test_test', 'left/eslint', 'right/eslint', 'leaf/eslint'},
        )):
            with self.subTest(macro=macro):
                files = self.workspace()
                files['web/tests/ya.make'] = f'{macro}\nEND()\n'
                files['web/src/unit.test.ts'] = 'it("works", () => {});\n'
                files['web/jest.config.js'] = 'module.exports = {};\n'
                graph = make(files, 'web/tests', '--target-platform-tests',
                             '--output-only-tests', '-DAUTOCHECK=yes', *PEER_TESTS)
                self.assertEqual(set(checks(graph)), expected)
                outputs = {out for node in graph['graph'] for out in node['outputs']}
                self.assertEqual('$(B)/web/output.tar' in outputs, 'JEST' in macro)

    def test_explicit_target_reexecutes_suite_metadata_after_peer_producer_reuse(self):
        for targets in (('web', 'left'), ('left', 'web')):
            with self.subTest(targets=targets):
                graph = make(self.workspace(), *targets, '--target-platform-tests',
                             '--output-only-tests', '-DAUTOCHECK=yes')
                self.assertEqual(set(checks(graph)), {'web/ts_lint_lint', 'left/eslint'})
                for output in ('package.json', 'output.tar'):
                    self.assertEqual(sum(f'$(B)/left/{output}' in n['outputs'] for n in graph['graph']), 1)


if __name__ == '__main__':
    unittest.main(verbosity=2)
