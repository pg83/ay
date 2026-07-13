#!/usr/bin/env python3

import contextlib
import importlib.util
import io
import json
import pathlib
import tempfile
import unittest
from unittest import mock


MODULE_PATH = pathlib.Path(__file__).with_name("perf.py")
SPEC = importlib.util.spec_from_file_location("ay_perf", MODULE_PATH)
PERF = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(PERF)


class PairedPermutationTest(unittest.TestCase):
    def test_exact_right_faster_sample(self):
        result = PERF.paired_permutation([-1.0, -2.0, -3.0])

        self.assertEqual(result.mean_log_ratio, -2.0)
        self.assertEqual(result.p_right_faster, 0.125)
        self.assertEqual(result.method, "exact")
        self.assertEqual(result.permutations, 8)

    def test_exact_wrong_direction_sample(self):
        result = PERF.paired_permutation([1.0, 2.0, 3.0])

        self.assertEqual(result.p_right_faster, 1.0)

    def test_exact_null_sample(self):
        result = PERF.paired_permutation([0.0, 0.0, 0.0])

        self.assertEqual(result.p_right_faster, 1.0)

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

    def test_mde_shrinks_with_candidate_block_count(self):
        noise = [-0.01, 0.01, -0.02, 0.02]

        small = PERF.minimum_detectable_effect(noise, 10, 0.01, 0.8)
        large = PERF.minimum_detectable_effect(noise, 40, 0.01, 0.8)

        self.assertGreater(small, large)
        self.assertAlmostEqual(small, 2 * large, delta=0.02)

    def test_required_runs_respect_exact_test_resolution(self):
        runs = PERF.required_runs_per_side([0.0, 0.0], 0.1, 0.01, 0.8)

        self.assertEqual(runs, 14)

    def test_holm_rejects_only_the_leading_family(self):
        rejected = PERF.holm_rejections(
            {
                "instructions": 0.0001,
                "cycles": 0.004,
                "task_clock": 0.02,
                "wall": 0.5,
            },
            0.01,
        )

        self.assertEqual(rejected, {"instructions"})

    def test_opposite_sign_tests_for_regression(self):
        values = [1.0, 2.0, 3.0]
        faster = PERF.paired_permutation(values)
        slower = PERF.paired_permutation([-value for value in values])

        self.assertEqual(faster.p_right_faster, 1.0)
        self.assertEqual(slower.p_right_faster, 0.125)


class RawMeasurementTest(unittest.TestCase):
    def test_perf_stat_parser(self):
        result = PERF._parse_perf_stat(
            "3525.47,msec,task-clock:u,1,100.00,CPUs utilized\n"
            "6520340988,,cycles:u,1,100.00,1.849,GHz\n"
            "11800939498,,instructions:u,1,100.00,1.81,insn per cycle\n"
        )

        self.assertEqual(result["task_clock"], 3.52547)
        self.assertEqual(result["cycles"], 6520340988)
        self.assertEqual(result["instructions"], 11800939498)

    def test_raw_blocks_ignore_incomplete_tail(self):
        samples = [
            {"side": "L", "wall": 1.0},
            {"side": "R", "wall": 2.0},
            {"side": "R", "wall": 2.0},
            {"side": "L", "wall": 4.0},
            {"side": "L", "wall": 7.0},
        ]

        self.assertEqual(PERF.raw_block_log_ratios(samples, "wall"), [0.0])

    def test_calibration_loader_keeps_only_raw_block_contrasts(self):
        samples = [
            {"side": "L", "cycles": 1},
            {"side": "R", "cycles": 2},
            {"side": "R", "cycles": 2},
            {"side": "L", "cycles": 4},
        ] * 2

        with tempfile.NamedTemporaryFile(mode="w+", encoding="utf-8") as stream:
            json.dump(samples, stream)
            stream.flush()
            result = PERF._load_calibration(stream.name, ("cycles",))

        self.assertEqual(result, {"cycles": [0.0, 0.0]})


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

    def test_perf_is_required(self):
        cpu = next(iter(PERF.os.sched_getaffinity(0)))
        argv = [
            "--left",
            PERF.sys.executable,
            "--right",
            PERF.sys.executable,
            "--cpu",
            str(cpu),
            "--",
            "target",
        ]

        with mock.patch.object(PERF.shutil, "which", return_value=None):
            with contextlib.redirect_stderr(io.StringIO()):
                with self.assertRaises(SystemExit):
                    PERF._parse_args(argv)


if __name__ == "__main__":
    unittest.main()
