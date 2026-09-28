import re
import signal
import subprocess
import tempfile
import unittest
from pathlib import Path

import lib


RED = "\x1b[31m"
RESET = "\x1b[0m"


def run_ay(*args, timeout=60):
    return subprocess.run(
        [str(lib.AY), *map(str, args)],
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
        timeout=timeout,
        check=False,
    )


class PerfTest(unittest.TestCase):
    def test_parser_benchmarks_c_sources_under_dir(self):
        with tempfile.TemporaryDirectory(prefix="ay-perf-parser-") as directory:
            root = Path(directory)
            (root / "sub").mkdir()
            sources = {
                "a.cpp": '#include "b.h"\nint main() { return 0; }\n',
                "sub/b.h": "#include <vector>\n",
            }
            for name, content in sources.items():
                (root / name).write_text(content)
            (root / "notes.txt").write_text("#include <ignored>\n")
            result = run_ay("dev", "perf", "parser", root)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertRegex(
            result.stdout,
            rf"\Afiles=2 bytes={sum(map(len, sources.values()))} "
            r"iters=[1-9][0-9]* per-pass=\S+ \([0-9]+ MB/s\)\n\Z",
        )

    def test_parser_usage_and_walk_errors(self):
        result = run_ay("dev", "perf", "parser")
        self.assertEqual(result.returncode, 2)
        self.assertEqual(result.stderr, "usage: ay perf parser <dir>\n")
        with tempfile.TemporaryDirectory(prefix="ay-perf-parser-") as directory:
            missing = Path(directory) / "missing"
            result = run_ay("dev", "perf", "parser", missing)
        self.assertEqual(result.returncode, 1)
        self.assertEqual(
            result.stderr,
            f"{RED}lstat {missing}: no such file or directory{RESET}\n",
        )

    def test_darts_matches_ancestor_walk(self):
        result = run_ay("dev", "perf", "darts")
        self.assertEqual(result.returncode, 0, result.stderr)
        lines = result.stdout.splitlines()
        self.assertEqual(lines[0], "roots=327 queries=2000, the trie agrees with the ancestor walk")
        self.assertRegex(lines[1], r"^darts: [0-9.]+ ns/op \(.+ total\)$")
        self.assertRegex(lines[2], r"^old:   [0-9.]+ ns/op \(.+ total\)$")
        self.assertEqual(lines[3:], ["sink=120376000"])

    def test_buckethash_stress_stops_cleanly_on_sigint(self):
        process = subprocess.Popen(
            [str(lib.AY), "dev", "perf", "buckethash"],
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
        )
        self.addCleanup(process.kill)
        seen = []
        for line in process.stdout:
            seen.append(line)
            if line.startswith("  ... "):
                process.send_signal(signal.SIGINT)
                break
        rest = process.stdout.read()
        self.assertEqual(process.wait(timeout=30), 0, process.stderr.read())
        self.assertEqual(seen[0], "corpus: 205 sequences, 1000724 elements\n")
        self.assertIn("=== pair collision stress ===\n", seen)
        self.assertRegex(seen[-1], r"^  \.\.\. [0-9]+ sequences, [0-9]+ distinct, 0 h1-collisions, no pair collision, [0-9]+s\n$")
        self.assertRegex(
            rest,
            r"\Ainterrupted after [0-9]+ sequences \([0-9]+ distinct, 0 h1-collisions\), "
            r"no pair collision, [0-9]+s\n\Z",
        )
        throughput = [line for line in seen if "ns/elem" in line]
        self.assertEqual(len(throughput), 6)
        self.assertTrue(all(
            re.search(r"sink=0x(dadddeab41ad8dcf|4be8c9084985e2e8)\)$", line.rstrip())
            for line in throughput
        ))


if __name__ == "__main__":
    unittest.main(verbosity=2)
