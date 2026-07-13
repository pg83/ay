#!/usr/bin/env python3

import importlib.util
import pathlib
import unittest


MODULE_PATH = pathlib.Path(__file__).with_name("perf.py")
SPEC = importlib.util.spec_from_file_location("ay_perf", MODULE_PATH)
PERF = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(PERF)


class MannWhitneyTest(unittest.TestCase):
    def test_exact_separated_samples(self):
        result = PERF.mann_whitney(list(range(10)), list(range(10, 20)))

        self.assertEqual(result.u_left, 0)
        self.assertEqual(result.rank_biserial, 1)
        self.assertAlmostEqual(result.p_two_sided, 0.00001082508822446903)
        self.assertEqual(result.method, "exact")

    def test_ties(self):
        result = PERF.mann_whitney([1, 1, 1], [1, 1, 1])

        self.assertEqual(result.u_left, 4.5)
        self.assertEqual(result.rank_biserial, 0)
        self.assertEqual(result.p_two_sided, 1)

    def test_normal_fallback_preserves_direction(self):
        result = PERF.mann_whitney(list(range(31)), list(range(31, 62)))

        self.assertEqual(result.u_left, 0)
        self.assertLess(result.p_two_sided, 1e-10)
        self.assertEqual(result.method, "normal")


class ScheduleTest(unittest.TestCase):
    def test_chess_order(self):
        self.assertEqual(PERF.run_order(0), ("left", "right"))
        self.assertEqual(PERF.run_order(1), ("right", "left"))
        self.assertEqual(PERF.run_order(2), ("left", "right"))

    def test_checkpoints(self):
        self.assertEqual(PERF.decision_checkpoints(30, 120, 1.5), [30, 45, 68, 102, 120])


if __name__ == "__main__":
    unittest.main()
