import unittest

from tst import lib


BARE = "NO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"


def fixture(declaration="UNITTEST_FOR(subject)", extra=""):
    return {
        "suite/ya.make": declaration + "\n" + BARE
        + "ADDINCL(extra)\n" + extra
        + "SRCS(local.cpp shared.cpp)\nEND()\n",
        "suite/local.cpp": '#include "api.h"\n',
        "subject/shared.cpp": '#include "api.h"\n',
        "subject/api.h": '#include "detail.h"\n',
        "subject/detail.h": "#pragma once\n",
        "subject/ya.make": "LIBRARY()\n" + BARE + "END()\n",
        "extra/.keep": "",
        "library/cpp/testing/unittest_main/ya.make": "LIBRARY()\n" + BARE + "END()\n",
    }


class UnittestForIncludesTest(unittest.TestCase):
    def test_tested_library_directory_is_a_local_include_and_source_root(self):
        graph = lib.make(fixture(), "suite")
        for output in ("$(B)/suite/local.cpp.o", "$(B)/suite/__/subject/shared.cpp.o"):
            with self.subTest(output=output):
                node = lib.node_by_output(graph, output)
                args = node["cmds"][0]["cmd_args"]
                self.assertEqual([a for a in args if a.startswith("-I")], [
                    "-I$(B)", "-I$(S)", "-I$(S)/subject", "-I$(S)/extra",
                ])
                self.assertIn("$(S)/subject/api.h", node["inputs"])
                self.assertIn("$(S)/subject/detail.h", node["inputs"])
        archive = lib.node_by_output(graph, "$(B)/suite/suite")
        self.assertNotIn("$(S)/subject/api.h", archive["inputs"])

    def test_explicit_include_of_tested_directory_is_deduplicated(self):
        graph = lib.make(fixture(extra="ADDINCL(subject)\n"), "suite")
        node = lib.node_by_output(graph, "$(B)/suite/local.cpp.o")
        args = node["cmds"][0]["cmd_args"]
        self.assertEqual(args.count("-I$(S)/subject"), 1)
        self.assertLess(args.index("-I$(S)/subject"), args.index("-I$(S)/extra"))

    def test_plain_unittest_peer_does_not_implicitly_add_include_directory(self):
        files = fixture("UNITTEST()", "PEERDIR(subject)\nSRCDIR(subject)\n")
        files["suite/local.cpp"] = ""
        graph = lib.make(files, "suite")
        node = lib.node_by_output(graph, "$(B)/suite/local.cpp.o")
        self.assertNotIn("-I$(S)/subject", node["cmds"][0]["cmd_args"])
        self.assertNotIn("$(S)/subject/api.h", node["inputs"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
