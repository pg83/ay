import subprocess
import unittest

import lib


def run_ay(*args):
    return subprocess.run(
        [str(lib.AY), *map(str, args)],
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
        timeout=60,
        check=False,
    )


class PerfLinkTest(unittest.TestCase):
    def test_link_benchmarks_each_materialization(self):
        result = run_ay("dev", "perf", "link", "500", "1")
        self.assertEqual(result.returncode, 0, result.stderr)
        lines = result.stdout.splitlines()
        self.assertEqual(lines[0], "materialize 500 files of 1 bytes from CAS:")
        self.assertEqual(len(lines), 4)
        for line, name in zip(lines[1:], ("link", "symlink", "copy")):
            self.assertRegex(
                line,
                rf"^{name} +500 files x +[0-9]+ iters: +[0-9]+ ns/op +[0-9]+ ops/s$",
            )

    def test_link_rejects_non_numeric_arguments(self):
        for args in (["x"], ["1", "y"]):
            result = run_ay("dev", "perf", "link", *args)
            self.assertEqual(result.returncode, 1)
            self.assertIn("strconv.Atoi: parsing", result.stderr)


if __name__ == "__main__":
    unittest.main(verbosity=2)
