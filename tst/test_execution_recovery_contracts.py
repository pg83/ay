import unittest

from test_exec_make import Workspace, built_kinds


class ExecutionRecoveryContractsTest(unittest.TestCase):
    def test_keep_going_caches_completed_nodes_when_external_test_runner_is_missing(self):
        ws = Workspace(self, {'app/ya.make': 'EXECTEST()\nRUN(check)\nSET(VALIDATE_DATA yes)\nDATA(sbr://12)\nEND()\n'})
        for attempt in range(2):
            with self.subTest(attempt=attempt):
                result = ws.make('-t', '-j', '1', '-k', targets=('app',), check=False)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn('$(TEST_TOOL_HOST)/test_tool', result.stderr)
                self.assertIn('--id 12', result.stderr)
                self.assertIn('validate_data_sbr', result.stderr)
                self.assertNotIn('panic:', result.stderr)
                kinds = [kind for kind, output in built_kinds(result.stderr)]
                self.assertEqual(kinds.count('UN'), int(attempt == 0))
        ws.meta_for('$(B)/app/app.pkg.fake')
        self.assertEqual(ws.server.requests, ['/py.tar'])


if __name__ == '__main__':
    unittest.main(verbosity=2)
