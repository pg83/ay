"""ALL_PY_SRCS glob union keeps each physical source once, in first-match order."""
import unittest

from tst import lib
from tst.make.test_all_python_forwarding import fixture, complete_nodes


SOURCES = ("__main__.py", "leaves/a.py", "leaves/deep/b.py", "leaves/test_skip.py", "twigs/c.py")


class AllPythonDiscoveryIdentityTest(unittest.TestCase):
    def paired(self, discovery, unique, kind="PY3_LIBRARY", main=False, sources=SOURCES):
        tail = "\nPY_SRCS(MAIN __main__.py)" if main else ""
        actual = lib.make(fixture(discovery + tail, kind=kind, sources=sources), "cedar/runner", "--sandboxing")
        expected = lib.make(fixture(unique + tail, kind=kind, sources=sources), "cedar/runner", "--sandboxing")
        self.assertEqual(complete_nodes(actual), complete_nodes(expected))
        self.assertEqual(actual["result"], expected["result"])

    def test_repeated_and_alias_directories_share_one_discovery(self):
        for dirs in ("leaves leaves", "./leaves leaves/../leaves"):
            with self.subTest(dirs=dirs):
                self.paired("ALL_PY_SRCS(RECURSIVE " + dirs + ")", "ALL_PY_SRCS(RECURSIVE leaves)", "PY3_PROGRAM", main=True)

    def test_recursive_overlap_preserves_first_source_identity_and_filters(self):
        for setting in ("RECURSIVE", "RECURSIVE NO_TEST_FILES"):
            with self.subTest(setting=setting):
                self.paired("ALL_PY_SRCS(" + setting + " leaves leaves/deep)", "ALL_PY_SRCS(" + setting + " leaves)")

    def test_repeated_discovery_crosses_resource_chunk_boundaries_once(self):
        sources = ("__main__.py",) + tuple("bough/module_" + str(i).zfill(3) + ".py" for i in range(70))
        self.paired("ALL_PY_SRCS(RECURSIVE bough bough)", "ALL_PY_SRCS(RECURSIVE bough)", "PY3_PROGRAM", main=True, sources=sources)

    def test_nonrecursive_order_namespace_and_python2_are_preserved(self):
        for discovery, unique, kind in (
            ("ALL_PY_SRCS(leaves leaves)", "ALL_PY_SRCS(leaves)", "PY3_LIBRARY"),
            ("ALL_PY_SRCS(NAMESPACE public.api twigs leaves twigs)", "ALL_PY_SRCS(NAMESPACE public.api twigs leaves)", "PY3_LIBRARY"),
            ("ALL_PY_SRCS(TOP_LEVEL leaves twigs leaves)", "ALL_PY_SRCS(TOP_LEVEL leaves twigs)", "PY3_LIBRARY"),
            ("ALL_PY_SRCS(RECURSIVE leaves leaves)", "ALL_PY_SRCS(RECURSIVE leaves)", "PY2_LIBRARY"),
        ):
            with self.subTest(discovery=discovery, kind=kind):
                self.paired(discovery, unique, kind)



if __name__ == "__main__":
    unittest.main(verbosity=2)
