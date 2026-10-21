import unittest

import lib
from test_link_peer_propagation import includes, link_args, link_group
from test_link_test_nodes import library


PEERS = ("build/cow/on", "library/cpp/malloc/jemalloc", "library/cpp/testing/unittest_main", "subject")
ARCHIVES = (
    "build/cow/on/libbuild-cow-on.a",
    "library/cpp/malloc/jemalloc/libcpp-malloc-jemalloc.a",
    "library/cpp/testing/unittest_main/libcpp-testing-unittest_main.a",
    "subject/libsubject.a",
)


def fixture(declaration):
    files = {}
    for peer in PEERS:
        name = peer.rsplit("/", 1)[-1]
        files[peer + "/ya.make"] = library(f"SRCS(peer.cpp)\nADDINCL(GLOBAL {peer}/inc)\n")
        files[peer + "/peer.cpp"] = "int peer;\n"
        files[peer + f"/inc/{name}.h"] = "#pragma once\n"
    files["check/ya.make"] = (declaration + "\n" + lib.BARE_MODULE
        + "ALLOCATOR(J)\nSRCS(local.cpp)\nEND()\n")
    files["check/local.cpp"] = '#include "on.h"\n#include "subject.h"\n'
    return files


class UnittestForPeerOrderTest(unittest.TestCase):
    def test_inherited_framework_and_program_peers_precede_tested_library(self):
        for targets in (("check",), ("subject", "check")):
            with self.subTest(targets=targets):
                graph = lib.make(fixture("UNITTEST_FOR(subject)"), *targets, "--sandboxing")
                self.assertEqual(link_group(link_args(graph, "$(B)/check/check")), list(ARCHIVES))
                self.assertEqual(includes(graph, "$(B)/check/local.cpp.o"), [
                    "-I$(B)", "-I$(S)", "-I$(S)/subject",
                    *[f"-I$(S)/{peer}/inc" for peer in PEERS],
                ])
                compiled = lib.node_by_output(graph, "$(B)/check/local.cpp.o")
                self.assertIn("$(S)/build/cow/on/inc/on.h", compiled["inputs"])
                self.assertIn("$(S)/subject/inc/subject.h", compiled["inputs"])
                linked = lib.node_by_output(graph, "$(B)/check/check")
                for archive in ARCHIVES:
                    node = lib.node_by_output(graph, "$(B)/" + archive)
                    self.assertIn(node["uid"], linked["deps"])
                    self.assertIn("$(B)/" + archive, linked["inputs"])

    def test_plain_unittest_explicit_peer_keeps_its_declared_order(self):
        graph = lib.make(fixture("UNITTEST()\nPEERDIR(subject)"), "check", "--sandboxing")
        self.assertEqual(link_group(link_args(graph, "$(B)/check/check")), list(ARCHIVES))
        self.assertEqual(includes(graph, "$(B)/check/local.cpp.o"), [
            "-I$(B)", "-I$(S)", *[f"-I$(S)/{peer}/inc" for peer in PEERS],
        ])


if __name__ == "__main__":
    unittest.main(verbosity=2)
