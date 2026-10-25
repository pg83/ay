import unittest

from tst import lib


BARE = "NO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"


def fixture(recursive="RECURSIVE", directories="left right left"):
    files = {
        "mod/ya.make": "LIBRARY()\n" + BARE
        + f"USRV_ALL_SRCS({recursive} {directories} EXCLUDE **/skip*)\nEND()\n",
    }
    for directory in ("left", "right"):
        for name in ("root.cpp", "a/z.cpp", "a/deep/x.cpp", "b/a.cpp", "skip.cpp", "a/skip.cpp"):
            files[f"mod/{directory}/{name}"] = ""
    return files


def archive_members(graph):
    archive = lib.node_by_output(graph, "$(B)/mod/libmod.a")
    return [a for cmd in archive["cmds"] for a in cmd["cmd_args"] if a.endswith(".o")]


class RecursiveNativeGlobsTest(unittest.TestCase):
    def test_recursive_sources_keep_pattern_and_breadth_first_directory_order(self):
        graph = lib.make(fixture(), "mod")
        self.assertEqual(archive_members(graph), [
            f"$(B)/mod/_/{directory}/{name}.o"
            for directory in ("left", "right")
            for name in ("a/z.cpp", "b/a.cpp", "a/deep/x.cpp", "root.cpp")
        ])
        self.assertEqual(len([n for n in graph["graph"] if n["kv"]["p"] == "CC"]), 8)

    def test_nonrecursive_sources_keep_requested_directory_order(self):
        graph = lib.make(fixture("", "right left"), "mod")
        self.assertEqual(archive_members(graph), [
            "$(B)/mod/_/right/root.cpp.o", "$(B)/mod/_/left/root.cpp.o",
        ])


if __name__ == "__main__":
    unittest.main(verbosity=2)
