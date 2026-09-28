import unittest

import lib


class ChaosLimitsTest(unittest.TestCase):
    """Internal limits that production inputs reach only at scale must not change the graph."""

    @classmethod
    def setUpClass(cls):
        cls.files = lib.layered_project()
        cls.expected = lib.make(cls.files, "app")

    def assert_same_graph(self, words):
        self.assertEqual(lib.make(self.files, "app", env={"AY_CHAOS": words}), self.expected)

    def test_minimal_tables_grow_without_losing_entries(self):
        self.assert_same_graph("table-hint=0")

    def test_epochs_wrap_around(self):
        for resets_before_wrap in (2, 16, 64, 256):
            with self.subTest(resets_before_wrap=resets_before_wrap):
                self.assert_same_graph(f"epoch-start={2**32 - resets_before_wrap}")

    def test_small_chunks_reach_the_chunk_cap(self):
        self.assert_same_graph("bump-chunk-bytes=64")

    def test_all_limits_at_once(self):
        self.assert_same_graph("table-hint=0 epoch-start=0xfffffffe bump-chunk-bytes=64")


if __name__ == "__main__":
    unittest.main(verbosity=2)
