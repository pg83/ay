from tst.make.lib import MakeTest


class PythonTwoRootPrefixesTest(MakeTest):
    def test_missing_sources_with_concatenated_roots_are_rejected(self):
        for root in ("BINDIR", "CURDIR", "ARCADIA_ROOT", "ARCADIA_BUILD_ROOT"):
            with self.subTest(root=root):
                self.write("ya.make", f"PY2_LIBRARY()\nPY_SRCS(${{{root}}}answer.py)\nEND()\n")
                self.assertNotEqual(self.make().returncode, 0)
