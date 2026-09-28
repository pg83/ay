import re
import subprocess
import sys
import unittest

import lib


RED = "\x1b[31m"
RESET = "\x1b[0m"
REPORT = re.compile(r"^maxrss \(subtree\): ([0-9]+) kB \([0-9]+\.[0-9] MiB\)$", re.M)
ALLOCATE = (
    "import sys, time\n"
    "block = bytearray(64 << 20)\n"
    "block[::4096] = b'x' * len(block[::4096])\n"
    "time.sleep(0.4)\n"
    "sys.exit(int(sys.argv[1]))\n"
)


def run_ay(*args, input=None):
    return subprocess.run(
        [str(lib.AY), *map(str, args)],
        input=input,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
        timeout=60,
        check=False,
    )


def peak_kb(stderr):
    match = REPORT.search(stderr)
    if match is None:
        raise AssertionError(f"no maxrss report in {stderr!r}")
    return int(match.group(1))


class MaxRSSTest(unittest.TestCase):
    def test_reports_subtree_peak_and_child_exit_code(self):
        script = f"{sys.executable} -c \"$0\" 7; exit $?"
        result = run_ay("dev", "maxrss", "--hz", "50", "--", "sh", "-c", script, ALLOCATE)
        self.assertEqual(result.returncode, 7, result.stderr)
        self.assertGreaterEqual(peak_kb(result.stderr), 64 << 10)

    def test_equals_form_and_implicit_command(self):
        result = run_ay("dev", "maxrss", "--hz=20", "--", sys.executable, "-c", ALLOCATE, "0")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertGreaterEqual(peak_kb(result.stderr), 64 << 10)
        result = run_ay(
            "dev", "maxrss", "sh", "-c", "echo out; echo err >&2; cat",
            input="from-stdin\n",
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout, "out\nfrom-stdin\n")
        self.assertTrue(result.stderr.startswith("err\n"))
        peak_kb(result.stderr)

    def test_usage_without_command(self):
        for args in ([], ["--"], ["--hz", "2", "--"]):
            result = run_ay("dev", "maxrss", *args)
            self.assertEqual(result.returncode, 2)
            self.assertEqual(
                result.stderr,
                "usage: ay dev maxrss [--hz N] -- <command> [args...]\n",
            )

    def test_start_failure(self):
        result = run_ay("dev", "maxrss", "--", "/nonexistent/ay-maxrss-cmd")
        self.assertEqual(result.returncode, 1)
        self.assertEqual(
            result.stderr,
            "maxrss: fork/exec /nonexistent/ay-maxrss-cmd: no such file or directory\n",
        )

    def test_flag_errors(self):
        cases = {
            ("--hz",): "maxrss: --hz needs a value",
            ("--hz", "0"): "maxrss: --hz must be > 0, got 0",
            ("--hz=-1",): "maxrss: --hz must be > 0, got -1",
            ("--hz", "abc"): 'strconv.ParseFloat: parsing "abc": invalid syntax',
            ("--bogus",): 'maxrss: unknown flag "--bogus"',
        }
        for args, message in cases.items():
            result = run_ay("dev", "maxrss", *args, "--", "true")
            self.assertEqual(result.returncode, 1)
            self.assertEqual(result.stderr, RED + message + RESET + "\n")


if __name__ == "__main__":
    unittest.main(verbosity=2)
