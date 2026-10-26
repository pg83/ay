import os
import tempfile
import unittest
from pathlib import Path

from tst.cli import ESC, RESET, USAGE_HEAD, run_ay


class DevtoolsCliTest(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory(prefix="ay-cli-test-")
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)

    def test_group_prefix_prints_group_usage(self):
        result = run_ay("dev", "dump")
        self.assertEqual(result.returncode, 2)
        text = result.stderr
        self.assertTrue(text.startswith(USAGE_HEAD + "dev dump <subcommand> [args]\n"))
        for name in ("diff", "grep"):
            self.assertIn(ESC + "[91mdev dump " + name + ":" + RESET, text)
        self.assertNotIn("dev perf", text)
        self.assertNotIn("make:", text)

    def graph_lines(self, count):
        source = self.root / "graph.jsonl"
        source.write_text("".join(f'{{"self_uid":"U{i:06d}","outputs":["/o{i}"]}}\n' for i in range(count)))
        return source

    def test_probe_flags_without_instrumentation_report_nothing(self):
        source = self.graph_lines(2)
        for flag in ("--probe=map", "--probe=callsite", "-probe=map"):
            result = run_ay(flag, "dev", "dump", "grep", "--in", source, "U000001")
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(result.stderr, "")
            self.assertIn('"self_uid": "U000001"', result.stdout)

    def test_callsite_out_env_appends_nothing_without_instrumentation(self):
        source = self.graph_lines(2)
        sites = self.root / "sites.txt"
        sites.write_text("pre-existing\n")
        env = dict(os.environ, CALLSITE_OUT=str(sites))
        result = run_ay("dev", "dump", "grep", "--in", source, "U000000", env=env)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(sites.read_text(), "pre-existing\n")
        env = dict(os.environ, CALLSITE_OUT=str(self.root / "missing/dir/sites"))
        result = run_ay("dev", "dump", "grep", "--in", source, "U000000", env=env)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertFalse((self.root / "missing").exists())

    def test_profiling_environment_writes_profiles(self):
        source = self.graph_lines(2000)
        cpu = self.root / "cpu.pprof"
        mem = self.root / "mem.pprof"
        env = dict(
            os.environ,
            YATOOL_CPUPROFILE=str(cpu),
            YATOOL_CPUHZ="500",
            YATOOL_MEMPROFILE=str(mem),
            YATOOL_MEMPROFRATE="1",
        )
        result = run_ay("dev", "dump", "grep", "--in", source, "U001999", env=env)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn('"self_uid": "U001999"', result.stdout)
        for profile in (cpu, mem):
            self.assertEqual(profile.read_bytes()[:2], b"\x1f\x8b", profile)


if __name__ == "__main__":
    unittest.main(verbosity=2)
