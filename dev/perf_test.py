#!/usr/bin/env python3

import importlib.util
import pathlib
import unittest


MODULE_PATH = pathlib.Path(__file__).with_name("perf.py")
SPEC = importlib.util.spec_from_file_location("ay_perf", MODULE_PATH)
PERF = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(PERF)


class PairedPermutationTest(unittest.TestCase):
    def test_exact_separated_sample(self):
        result = PERF.paired_permutation([1.0, 2.0, 3.0])

        self.assertEqual(result.mean_log_ratio, 2.0)
        self.assertEqual(result.p_two_sided, 0.25)
        self.assertEqual(result.method, "exact")
        self.assertEqual(result.permutations, 8)

    def test_exact_null_sample(self):
        result = PERF.paired_permutation([0.0, 0.0, 0.0])

        self.assertEqual(result.p_two_sided, 1.0)

    def test_sampled_test_is_reproducible(self):
        values = [0.1, 0.2, 0.3]
        first = PERF.paired_permutation(values, exact_max_blocks=2, permutations=1000, seed=17)
        second = PERF.paired_permutation(values, exact_max_blocks=2, permutations=1000, seed=17)

        self.assertEqual(first, second)
        self.assertEqual(first.method, "sampled")
        self.assertEqual(first.permutations, 1000)

    def test_block_log_ratio_cancels_linear_log_drift(self):
        # L occupies the outer positions and R the inner positions.  With equal
        # binaries and linear drift in log(time), the block contrast is zero.
        times = [1.0, 2.0, 4.0, 8.0]

        self.assertAlmostEqual(
            PERF.block_log_ratio([times[0], times[3]], [times[1], times[2]]),
            0.0,
        )

    def test_summary_reports_minimum_wall_times(self):
        result = PERF.summarize([3.0, 1.0], [4.0, 2.0], [0.1])

        self.assertEqual(result.left_min, 1.0)
        self.assertEqual(result.right_min, 2.0)


class ScheduleTest(unittest.TestCase):
    def test_block_order(self):
        self.assertEqual(
            PERF.block_order(False), ("left", "right", "right", "left")
        )
        self.assertEqual(
            PERF.block_order(True), ("right", "left", "left", "right")
        )

    def test_checkpoints(self):
        self.assertEqual(
            PERF.decision_checkpoints(30, 120, 1.5), [30, 46, 70, 106, 120]
        )

    def test_checkpoints_require_complete_blocks(self):
        with self.assertRaisesRegex(ValueError, "must be even"):
            PERF.decision_checkpoints(31, 120, 1.5)


if __name__ == "__main__":
    unittest.main()
