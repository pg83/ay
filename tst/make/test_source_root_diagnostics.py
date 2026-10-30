import subprocess

from tst.backends import ay_only
from tst.make.lib import MakeTest, PROJECT


@ay_only("ay source-root and injected filesystem diagnostics; no graph assertions")
class SourceRootDiagnosticsTest(MakeTest):
    def setUp(self):
        super().setUp()
        self.write("ya.make", "LIBRARY()\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")

    def test_nonexistent_source_roots_are_reported(self):
        result = subprocess.run(
            [self.binary, "make", "-j0", "--source-root", str(self.work / "missing"), PROJECT],
            cwd=self.work, capture_output=True, text=True, timeout=30,
        )
        self.assertEqual(result.returncode, 1, result.stderr)
        self.assertIn("open source root", result.stderr)
        self.assertIn("no such file or directory", result.stderr)

    def test_targets_are_required_outside_the_source_root(self):
        result = subprocess.run(
            [self.binary, "make", "-j0", "--source-root", str(self.root)],
            cwd=self.work, capture_output=True, text=True, timeout=30,
        )
        self.assertEqual(result.returncode, 1, result.stderr)
        self.assertIn("no targets supplied", result.stderr)

    def test_directory_read_errors_are_reported(self):
        result = self.make(dump_graph=False, env={"AY_CHAOS": "getdents-eio=0"})
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("no tool chain description", result.stderr)
        self.assertNotIn("panic:", result.stderr)

    def test_unknown_host_architectures_report_toolchain_errors(self):
        result = self.make(dump_graph=False, env={"AY_CHAOS": "host-arch=e2e_unknown"})
        self.assertEqual(result.returncode, 1, result.stderr)
        self.assertIn("e2e_unknown", result.stderr.lower())

    def test_cached_data_read_errors_are_reported(self):
        self.write("ya.make", f"EXECTEST()\nDATA(arcadia/{PROJECT}/value.txt)\nRUN(echo answer)\nEND()\n")
        self.write("value.txt", "42\n")
        result = self.make("-t", "--cache-tests", dump_graph=False, env={"AY_CHAOS": "test-data-read-eio=0"})
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("input/output error", result.stderr)
