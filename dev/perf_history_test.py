#!/usr/bin/env python3

import importlib.util
import json
import pathlib
import sys
import tempfile
import unittest
from unittest import mock


MODULE_PATH = pathlib.Path(__file__).with_name("perf_history.py")
SPEC = importlib.util.spec_from_file_location("ay_perf_history", MODULE_PATH)
HISTORY = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = HISTORY
SPEC.loader.exec_module(HISTORY)


class HistoryTest(unittest.TestCase):
    def test_history_uses_first_parent(self):
        raw = (
            "b" * 40 + "\0" + "a" * 40 + " " + "f" * 40
            + "\0" + "2026-07-12T01:00:00+03:00\0merge subject"
        )

        with mock.patch.object(HISTORY, "_git", return_value=raw):
            pairs = HISTORY.history_pairs(pathlib.Path("/repo"), "date", "HEAD")

        self.assertEqual(len(pairs), 1)
        self.assertEqual(pairs[0].left, "a" * 40)
        self.assertEqual(pairs[0].right, "b" * 40)
        self.assertEqual(pairs[0].subject, "merge subject")

    def test_completed_attempt_is_resumed(self):
        with tempfile.TemporaryDirectory() as tmp:
            pair = pathlib.Path(tmp)
            incomplete = pair / "attempt-001"
            complete = pair / "attempt-002"
            incomplete.mkdir()
            complete.mkdir()
            (incomplete / "result.json").write_text(
                json.dumps({"exit_code": 130}), encoding="utf-8"
            )
            (complete / "result.json").write_text(
                json.dumps({"exit_code": 2, "status": "inconclusive"}),
                encoding="utf-8",
            )

            attempt, result = HISTORY._latest_completed_attempt(pair)

        self.assertEqual(attempt.name, "attempt-002")
        self.assertEqual(result["status"], "inconclusive")

    def test_summary_is_derived_from_raw_blocks(self):
        samples = []
        metrics = ("wall", "user", "system", "task_clock", "cycles", "instructions")

        for _ in range(8):
            for side, value in (("L", 10), ("R", 9), ("R", 9), ("L", 10)):
                samples.append({"side": side, **{metric: value for metric in metrics}})

        options = {
            "min_runs": 16,
            "max_runs": 16,
            "growth": 1.5,
            "alpha": 0.05,
            "exact_max_blocks": 20,
            "permutations": 1000,
        }
        result = HISTORY._effect_summary(samples, 17, options)

        self.assertEqual(result["runs_per_side"], 16)
        self.assertAlmostEqual(result["metrics"]["wall"]["effect_pct"], -10.0)
        self.assertIn("wall", result["improved"])
        self.assertEqual(result["regressed"], [])

    def test_pair_seed_is_stable_and_directional(self):
        pair = HISTORY.CommitPair(1, "a" * 40, "b" * 40, "date", "subject")
        reverse = HISTORY.CommitPair(1, "b" * 40, "a" * 40, "date", "subject")

        self.assertEqual(HISTORY._pair_seed(pair), HISTORY._pair_seed(pair))
        self.assertNotEqual(HISTORY._pair_seed(pair), HISTORY._pair_seed(reverse))


if __name__ == "__main__":
    unittest.main()
