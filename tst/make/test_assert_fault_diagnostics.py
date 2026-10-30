from tst.backends import ay_only
from tst.make.lib import MakeTest


@ay_only("ay assertion fault diagnostics without graph assertions")
class AssertFaultDiagnosticsTest(MakeTest):
    def test_injected_assertion_failure_is_reported_as_a_cli_error(self):
        self.write("ya.make", "LIBRARY()\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        result = self.make(dump_graph=False, env={"AY_CHAOS": "assert=256"})
        self.assertEqual(result.returncode, 1, result.stderr)
        self.assertTrue(result.stderr.strip())
        self.assertNotIn("panic:", result.stderr)
