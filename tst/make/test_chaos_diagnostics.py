from tst.backends import ay_only
from tst.make.lib import MakeTest


@ay_only("ay chaos fault diagnostics; no graph assertions")
class ChaosDiagnosticsTest(MakeTest):
    def setUp(self):
        super().setUp()
        self.write("ya.make", "LIBRARY()\nSRCS(answer.cpp)\nEND()\n")
        includes = "".join(f'#include "header{index}.h"\n' for index in range(32))
        self.write("answer.cpp", includes + "int answer() { return 42; }\n")
        self.write("common.h", "#pragma once\n")
        for index in range(32):
            self.write(f"header{index}.h", '#pragma once\n#include "common.h"\n')

    def test_invalid_chaos_options_are_reported_before_execution(self):
        for chaos in ("unknown-point=1", "epoch-start=bad", "epoch-start"):
            with self.subTest(chaos=chaos):
                result = self.make(dump_graph=False, env={"AY_CHAOS": chaos})
                self.assertEqual(result.returncode, 2, result.stderr)
                self.assertIn("AY_CHAOS: bad word", result.stderr)
                self.assertEqual(result.stdout, "")

    def test_full_slice_hash_collisions_are_reported(self):
        result = self.make(dump_graph=False, env={"AY_CHAOS": "xxh-slice-hi=0 xxh-slice-lo=0"})
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("SliceCache: hash pair collision", result.stderr)

    def test_partial_slice_hash_collisions_detect_inconsistent_overflow_entries(self):
        for seed in range(8):
            with self.subTest(seed=seed):
                result = self.make(dump_graph=False, env={"AY_CHAOS": f"xxh-slice-hi=3 xxh-slice-lo=3 intern-seed={seed}"})
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("SliceCache: hash pair collision", result.stderr)

    def test_include_bucket_hash_pair_collisions_are_reported(self):
        result = self.make(env={"AY_CHAOS": "hash-bucket-h1=3 hash-bucket-h2=3"})
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("BucketCache: bucket hash pair collision", result.stderr)

    def test_include_bucket_list_hash_pair_collisions_are_reported(self):
        result = self.make(env={"AY_CHAOS": "hash-list-h1=3 hash-list-h2=3"})
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("BucketCache: bucket-list hash pair collision", result.stderr)

    def test_foreign_panics_are_rethrown_with_the_original_value(self):
        result = self.make(dump_graph=False, env={"AY_CHAOS": "foreign-panic=0"})
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("panic: chaos: foreign panic", result.stderr)
