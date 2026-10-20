import json
import sys
import unittest

import lib


@unittest.skipUnless(sys.platform.startswith("linux"), "the seam wraps Linux syscalls")
class ChaosSyscallsTest(unittest.TestCase):
    """Filesystem syscalls behind the seam: retries and fallbacks keep the graph."""

    @classmethod
    def setUpClass(cls):
        cls.files = lib.layered_project()
        cls.expected = lib.make(cls.files, "app")

    def assert_same_graph(self, words):
        self.assertEqual(lib.make(self.files, "app", env={"AY_CHAOS": words}), self.expected)

    def test_interrupted_calls_are_retried(self):
        self.assert_same_graph(
            "eintr-open=0 eintr-openat=0 eintr-openat=5 eintr-read=0 eintr-read=3 "
            "eintr-getdents=0 eintr-getdents=2"
        )

    def test_entries_without_a_type_are_stated(self):
        self.assert_same_graph("dirent-unknown=1")
        self.assert_same_graph("dirent-unknown=1 eintr-fstatat=0 eintr-fstatat=4")

    def test_unreadable_directory_hides_its_files(self):
        result = lib.make_process(self.files, "app", env={"AY_CHAOS": "getdents-eio=8"})
        self.assertEqual(result.returncode, 1, result.stderr)
        self.assertEqual(result.stdout, "")
        self.assertIn("missing-include:", result.stderr)
        self.assertIn("unresolved include", result.stderr)
        self.assertIn("not found in source, build, search path, or sysincl", result.stderr)


if __name__ == "__main__":
    unittest.main(verbosity=2)
