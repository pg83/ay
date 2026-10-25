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
        for name in ("normalize", "sort", "diff", "grep"):
            self.assertIn(ESC + "[91mdev dump " + name + ":" + RESET, text)
        self.assertNotIn("dev perf", text)
        self.assertNotIn("make:", text)

    def test_probe_flags_without_instrumentation_report_nothing(self):
        source = self.root / "in.txt"
        source.write_text("b\na\n")
        for flag in ("--probe=map", "--probe=callsite", "-probe=map"):
            output = self.root / "out.txt"
            result = run_ay(flag, "dev", "dump", "sort", "--in", source, "--out", output)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(result.stderr, "")
            self.assertEqual(output.read_text(), "a\nb\n")

    def test_callsite_out_env_appends_nothing_without_instrumentation(self):
        source = self.root / "in.txt"
        source.write_text("b\na\n")
        sites = self.root / "sites.txt"
        sites.write_text("pre-existing\n")
        env = dict(os.environ, CALLSITE_OUT=str(sites))
        result = run_ay(
            "dev", "dump", "sort", "--in", source, "--out", self.root / "o1",
            env=env,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(sites.read_text(), "pre-existing\n")
        env = dict(os.environ, CALLSITE_OUT=str(self.root / "missing/dir/sites"))
        result = run_ay(
            "dev", "dump", "sort", "--in", source, "--out", self.root / "o2",
            env=env,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertFalse((self.root / "missing").exists())

    def test_profiling_environment_writes_profiles(self):
        source = self.root / "in.txt"
        source.write_text("".join(f"{i:06d}\n" for i in range(2000, 0, -1)))
        output = self.root / "out.txt"
        cpu = self.root / "cpu.pprof"
        mem = self.root / "mem.pprof"
        env = dict(
            os.environ,
            YATOOL_CPUPROFILE=str(cpu),
            YATOOL_CPUHZ="500",
            YATOOL_MEMPROFILE=str(mem),
            YATOOL_MEMPROFRATE="1",
        )
        result = run_ay(
            "dev", "dump", "sort", "--in", source, "--out", output,
            "--chunk-bytes", "64",
            env=env,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(output.read_text().splitlines()[:2], ["000001", "000002"])
        for profile in (cpu, mem):
            self.assertEqual(profile.read_bytes()[:2], b"\x1f\x8b", profile)


if __name__ == "__main__":
    unittest.main(verbosity=2)
