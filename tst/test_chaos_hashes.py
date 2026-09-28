import os
import unittest

import lib


def plain(text):
    return text.replace("\x1b[31m", "").replace("\x1b[33m", "").replace("\x1b[0m", "")


class ChaosHashesTest(unittest.TestCase):
    """Hashes behind the seam: narrowed halves collide on real inputs."""

    @classmethod
    def setUpClass(cls):
        cls.files = lib.layered_project()
        cls.expected = lib.make(cls.files, "app")

    def assert_same_graph(self, words):
        self.assertEqual(lib.make(self.files, "app", env={"AY_CHAOS": words}), self.expected)

    def test_colliding_keys_keep_the_graph(self):
        for words in ("xxh-intern-hi=0", "xxh-intern-hi=15", "xxh-slice-hi=0",
                      "hash-bucket-h1=0", "hash-list-h1=0"):
            with self.subTest(words=words):
                self.assert_same_graph(words)

    def test_zero_verify_halves_keep_the_graph(self):
        for words in ("xxh-intern-lo=0", "xxh-slice-lo=0", "hash-bucket-h2=0", "hash-list-h2=0"):
            with self.subTest(words=words):
                self.assert_same_graph(words)

    def test_bucket_hash_without_avx512_keeps_the_graph(self):
        # Closure buckets of 16 elements and more take the SIMD path.
        files = {
            "wide/ya.make": "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(a.cpp)\nEND()\n",
            "wide/a.cpp": "".join(f'#include "h{i}.h"\n' for i in range(300)),
            **{f"wide/h{i}.h": "" for i in range(300)},
        }
        self.assertEqual(
            lib.make(files, "wide", env={"AY_CHAOS": "cpu-avx512=0"}),
            lib.make(files, "wide"),
        )

    def test_epoch_wraparound_clears_overflowed_buckets(self):
        for resets_before_wrap in (16, 64, 256):
            with self.subTest(resets_before_wrap=resets_before_wrap):
                self.assert_same_graph(f"hash-bucket-h1=0 epoch-start={2**32 - resets_before_wrap}")

    def test_bucket_overflow_is_reported_in_verbose_mode(self):
        result = lib.make_process(self.files, "app", "--verbose", env={"AY_CHAOS": "hash-bucket-h1=0"})
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertRegex(
            plain(result.stderr),
            r"(?m)^bucket-hash: \d+ h1 mismatches, \d+ buckets in overflow "
            r"\(pair-collision headroom shrinking\)$",
        )

    def test_pairs_colliding_in_both_halves_are_rejected(self):
        wide = lib.wide_project(20)
        for files, words, message in (
            (self.files, "hash-bucket-h1=3 hash-bucket-h2=7",
             r"BucketCache: bucket hash pair collision \(h1=0x[0-9a-f]+ h2=0x[0-9a-f]+, \d+ elems\)"),
            (self.files, "hash-list-h1=3 hash-list-h2=3",
             r"BucketCache: bucket-list hash pair collision \(h1=0x[0-9a-f]+ h2=0x[0-9a-f]+, \d+ buckets\)"),
            (wide, "xxh-slice-hi=3 xxh-slice-lo=7",
             r"SliceCache: hash pair collision \(h1=0x[0-9a-f]+ h2=0x[0-9a-f]+, \d+ elems\)"),
        ):
            with self.subTest(words=words):
                result = lib.make_process(files, "app", env={"AY_CHAOS": words})
                self.assertEqual(result.returncode, 1, result.stderr)
                self.assertEqual(result.stdout, "")
                self.assertRegex(plain(result.stderr), rf"\A{message}\n\Z")


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
