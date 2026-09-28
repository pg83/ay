import hashlib
import os
import subprocess
import tempfile
import unittest
from pathlib import Path

import lib


RED = "\x1b[31m"
RESET = "\x1b[0m"


EXPECTED_256 = (
    "cas analyze: cas   (content-defined chunking, avg≈256 B, min-len 100 B)\n"
    "  files            4   (>= min-len)\n"
    "  total size       46.88 KiB   (whole-file CAS, already content-deduped)\n"
    "  chunks           158   (avg 303 B)\n"
    "  unique chunks    102   (64.6% of chunks)\n"
    "  unique data      30.33 KiB   (64.7% of total)\n"
    "  + chunk refs     4.94 KiB   (158 × 32 B)\n"
    "  chunked store    35.26 KiB   (75.2% of total)\n"
    "  saving           11.61 KiB   (24.8%)\n"
)

EXPECTED_DEFAULT = (
    "cas analyze: cas   (content-defined chunking, avg≈8192 B, min-len 0 B)\n"
    "  files            5   (>= min-len)\n"
    "  total size       46.88 KiB   (whole-file CAS, already content-deduped)\n"
    "  chunks           9   (avg 5333 B)\n"
    "  unique chunks    8   (88.9% of chunks)\n"
    "  unique data      37.48 KiB   (79.9% of total)\n"
    "  + chunk refs     288 B   (9 × 32 B)\n"
    "  chunked store    37.76 KiB   (80.5% of total)\n"
    "  saving           9.12 KiB   (19.5%)\n"
)

EXPECTED_64 = (
    "cas analyze: cas   (content-defined chunking, avg≈64 B, min-len 3.91 KiB)\n"
    "  files            3   (>= min-len)\n"
    "  total size       43.95 KiB   (whole-file CAS, already content-deduped)\n"
    "  chunks           363   (avg 123 B)\n"
    "  unique chunks    225   (62.0% of chunks)\n"
    "  unique data      25.40 KiB   (57.8% of total)\n"
    "  + chunk refs     11.34 KiB   (363 × 32 B)\n"
    "  chunked store    36.74 KiB   (83.6% of total)\n"
    "  saving           7.21 KiB   (16.4%)\n"
)


def blob(seed, size):
    out = b""
    counter = 0
    while len(out) < size:
        out += hashlib.sha256(f"{seed}-{counter}".encode()).digest()
        counter += 1
    return out[:size]


class CasAnalyzeTest(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory(prefix="ay-cas-test-")
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        cas = self.root / "cas"
        (cas / "sub").mkdir(parents=True)
        shared = blob("a", 20000)
        (cas / "a.bin").write_bytes(shared)
        (cas / "sub/a-edited.bin").write_bytes(shared[:15000] + blob("tail", 5000))
        (cas / "b.bin").write_bytes(blob("b", 3000))
        (cas / "zeros.bin").write_bytes(bytes(5000))
        (cas / "small.bin").write_bytes(b"tiny")
        (cas / "link.bin").symlink_to("a.bin")

    def analyze(self, *args):
        return subprocess.run(
            [str(lib.AY), "dev", "cas", *args],
            cwd=self.root,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
            timeout=60,
            check=False,
        )

    def test_reports_chunk_dedup_savings(self):
        result = self.analyze("analyze", "--chunk=256", "--min-len=100", "cas")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stderr, "")
        self.assertEqual(result.stdout, EXPECTED_256)

    def test_default_chunk_size_counts_small_files(self):
        result = self.analyze("analyze", "cas")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout, EXPECTED_DEFAULT)

    def test_minimum_chunk_floor(self):
        result = self.analyze("analyze", "--chunk=64", "--min-len=4000", "cas")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout, EXPECTED_64)

    def test_missing_directory_reports_walk_error(self):
        result = self.analyze("analyze", "missing")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(
            result.stderr,
            "cas analyze: walk: lstat missing: no such file or directory\n",
        )
        self.assertEqual(result.stdout, "cas analyze: missing — no files >= min-len\n")

    @unittest.skipIf(os.geteuid() == 0, "root ignores file permissions")
    def test_unreadable_file_is_skipped(self):
        unreadable = self.root / "cas/zeros.bin"
        unreadable.chmod(0)
        self.addCleanup(unreadable.chmod, 0o644)
        result = self.analyze("analyze", "--min-len=4000", "cas")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(
            result.stderr,
            "cas analyze: skip cas/zeros.bin: open cas/zeros.bin: permission denied\n",
        )
        self.assertIn("  files            3   (>= min-len)\n", result.stdout)

    def test_argument_errors(self):
        cases = {
            (): "usage: ay dev cas analyze [--chunk=N] <cas-dir>",
            ("stats",): "usage: ay dev cas analyze [--chunk=N] <cas-dir>",
            ("analyze",): "cas analyze: missing <cas-dir>",
            ("analyze", "--chunk=32", "cas"): "cas analyze: --chunk must be >= 64",
            ("analyze", "--bogus", "cas"): 'cas analyze: unknown flag "--bogus"',
            ("analyze", "--chunk=x", "cas"): 'strconv.Atoi: parsing "x": invalid syntax',
            ("analyze", "--min-len=x", "cas"): 'strconv.ParseInt: parsing "x": invalid syntax',
        }
        for args, message in cases.items():
            result = self.analyze(*args)
            self.assertEqual(result.returncode, 1)
            self.assertEqual(result.stderr, RED + message + RESET + "\n")


if __name__ == "__main__":
    unittest.main(verbosity=2)
