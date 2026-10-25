import os
import unittest

from tst import lib


class ChaosPerfBucketHashTest(unittest.TestCase):
    def test_stress_reports_the_first_pair_collision(self):
        result = lib.run_process(
            "dev", "perf", "buckethash", timeout=120,
            env={**os.environ, "AY_CHAOS": "hash-bucket-h1=3 hash-bucket-h2=7"},
        )
        self.assertEqual(result.returncode, 1, result.stderr)
        self.assertRegex(
            result.stdout.splitlines()[-1],
            r"^PAIR COLLISION after \d+ sequences \(\d+ distinct, [0-9.]+s\): "
            r"h1=0x[0-9a-f]{16} h2=0x[0-9a-f]{16}$",
        )


if __name__ == "__main__":
    unittest.main(verbosity=2)
