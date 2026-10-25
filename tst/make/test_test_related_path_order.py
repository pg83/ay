"""DATA related paths are canonicalized before native set ordering."""
import unittest

from tst import lib
from tst.make.test_python_srcs import python_base


def fixture(data, python=False):
    files = python_base() if python else {}
    body = "PY3TEST()\n" + lib.BARE_MODULE + "NO_CHECK_IMPORTS()\nTEST_SRCS(test_leaf.py)\n" if python else "EXECTEST()\nRUN(echo)\n"
    files["maple/ya.make"] = body + "DATA(" + data + ")\nEND()\n"
    files["maple/test_leaf.py"] = "def test_leaf(): pass\n"
    files["library/python/pytest/ya.make"] = "LIBRARY()\n" + lib.BARE_MODULE + "END()\n"
    files.update({"orchard/elm/child.txt": "leaf\n", "orchard/elm-branch/leaves/one.txt": "branch\n", "orchard/elm.txt": "file\n", "orchard/birch.txt": "other\n"})
    return files


def related(node):
    args = [a for c in node["cmds"] for a in c["cmd_args"]]
    return [args[i + 1] for i, value in enumerate(args) if value == "--test-related-path"]


class TestRelatedPathOrderTest(unittest.TestCase):
    def check_paths(self, data, expected, python=False):
        graph = lib.make(fixture(data, python), "maple", "-t", "--sandboxing")
        suite = "py3test" if python else "exectest"
        node = lib.node_by_output(graph, "$(B)/maple/test-results/" + suite + "/meta.json")
        self.assertEqual(related(node), ["$(S)/maple", *["$(S)/" + path for path in expected]])
        self.assertTrue(graph["result"])
        for path in expected:
            self.assertIn("$(S)/" + path, node["inputs"])
        return graph

    def test_trailing_slashes_do_not_reverse_directory_prefix_order(self):
        for python in (False, True):
            with self.subTest(python=python):
                self.check_paths("arcadia/orchard/elm-branch/leaves/ arcadia/orchard/elm/",
                                 ["orchard/elm", "orchard/elm-branch/leaves"], python)

    def test_files_directory_prefixes_and_canonical_aliases_sort_after_cleaning(self):
        self.check_paths("arcadia/orchard/elm/child.txt arcadia/orchard/elm.txt arcadia/orchard/elm-branch/leaves/ arcadia/orchard/elm/ arcadia/orchard/./elm// arcadia/orchard/birch.txt arcadia/orchard/elm",
                         ["orchard/birch.txt", "orchard/elm", "orchard/elm-branch/leaves", "orchard/elm.txt", "orchard/elm/child.txt"])

    def test_already_canonical_data_order_is_preserved(self):
        self.check_paths("arcadia/orchard/elm/child.txt arcadia/orchard/birch.txt arcadia/orchard/elm.txt",
                         ["orchard/birch.txt", "orchard/elm.txt", "orchard/elm/child.txt"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
