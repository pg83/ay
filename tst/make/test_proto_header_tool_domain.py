"""Every generated C header consumes its PB producer's shared tool domain."""
import unittest

from tst import lib
from tst.make.test_grpc_cpp_include_scope import APPHOST, fixture as base_fixture, markers
from tst.make.test_proto_cpp import pb_node


def fixture(suffixes=(".leaf.hpp",), grpc=True, transitive=False):
    files = base_fixture(grpc=grpc, transitive=transitive)
    lib.tool_program(files, "tools/branchmaker", "branch_driver")
    files["tools/branchmaker/ya.make"] = files["tools/branchmaker/ya.make"].replace("END()", "INDUCED_DEPS(h rt/branch_header.h)\nINDUCED_DEPS(cpp rt/branch_cpp.h)\nINDUCED_DEPS(h+cpp rt/branch_common.h)\nEND()")
    for name in ("branch_header", "branch_cpp", "branch_common"):
        files["rt/" + name + ".h"] = "#pragma once\n"
    macro = "CPP_PROTO_PLUGIN2" if len(suffixes) == 2 else "CPP_PROTO_PLUGIN"
    files["cedar/model/ya.make"] = files["cedar/model/ya.make"].replace("SRCS(entry.proto)", macro + "(branch tools/branchmaker " + " ".join(suffixes) + ")\nSRCS(entry.proto)")
    for index, suffix in enumerate(suffixes):
        if suffix.endswith((".cc", ".cpp")):
            continue
        files["cedar/read/generic_" + str(index) + ".cpp"] = '#include "cedar/model/entry' + suffix + '"\n'
        files["cedar/read/ya.make"] = files["cedar/read/ya.make"].replace("SRCS(", "SRCS(generic_" + str(index) + ".cpp ")
    return files


class ProtoHeaderToolDomainTest(unittest.TestCase):
    def test_builtin_and_generic_header_readers_share_the_producer_header_domain(self):
        for suffixes in ((".leaf.hpp",), (".leaf.cuh",), (".leaf.hpp", ".branch.hh"),
                         (".grpc.pb.cc", ".leaf.hpp"), (".leaf.hpp", ".grpc.pb.cc")):
            for grpc in (False, True):
                # A generic suffix may supply the GRPC C++ output itself.
                if grpc and ".grpc.pb.cc" in suffixes:
                    continue
                with self.subTest(suffixes=suffixes, grpc=grpc):
                    graph = lib.make(fixture(suffixes, grpc), "cedar/read", "cedar/model", "--sandboxing")
                    expected = {"$(S)/rt/pb_common.h", "$(S)/rt/facet_h.h", "$(S)/rt/nested.h", "$(S)/rt/branch_header.h", "$(S)/rt/branch_common.h"}
                    if grpc:
                        expected.update(("$(S)/rt/grpc_h.h", "$(S)/rt/grpc_common.h"))
                    producer = pb_node(graph, "$(B)/cedar/model/entry.pb.h")
                    names = ["pb_h", "apphost_h"] + (["grpc_pb_h"] if grpc else [])
                    names += ["generic_" + str(i) for i, suffix in enumerate(suffixes) if not suffix.endswith((".cc", ".cpp"))]
                    for name in names:
                        node = lib.node_by_output(graph, "$(B)/cedar/read/" + name + ".cpp.o")
                        self.assertEqual(markers(node), expected)
                        self.assertIn(producer["uid"], node["deps"])
                        for owner in ("base", "deep"):
                            header = "$(B)/cedar/" + owner + "/entry.pb.h"
                            self.assertIn(header, node["inputs"])
                            self.assertIn(pb_node(graph, header)["uid"], node["deps"])
                    for source in ("entry.pb.cc.o",) + (("entry.grpc.pb.cc.o",) if grpc or ".grpc.pb.cc" in suffixes else ()):
                        cpp = lib.node_by_output(graph, "$(B)/cedar/model/" + source)
                        self.assertNotIn("$(S)/rt/branch_header.h", cpp["inputs"])
                        self.assertNotIn("$(S)/rt/facet_h.h", cpp["inputs"])

    def test_header_without_imports_consumes_the_same_tool_domain(self):
        files = fixture()
        files["cedar/model/entry.proto"] = 'syntax = "proto2"; message Root {}\n'
        graph = lib.make(files, "cedar/read", "cedar/model", "--sandboxing")
        for name in ("pb_h", "apphost_h", "grpc_pb_h", "generic_0"):
            reader = lib.node_by_output(graph, "$(B)/cedar/read/" + name + ".cpp.o")
            self.assertEqual(markers(reader), {"$(S)/rt/pb_common.h", "$(S)/rt/facet_h.h", "$(S)/rt/nested.h", "$(S)/rt/branch_header.h", "$(S)/rt/branch_common.h", "$(S)/rt/grpc_h.h", "$(S)/rt/grpc_common.h"})

    def test_non_header_output_processors_do_not_consume_the_header_domain(self):
        for suffix in (".leaf.txt", ".leaf.py"):
            with self.subTest(suffix=suffix):
                graph = lib.make(fixture((suffix,)), "cedar/read", "cedar/model", "--sandboxing")
                reader = lib.node_by_output(graph, "$(B)/cedar/read/generic_0.cpp.o")
                self.assertEqual(markers(reader), set())


if __name__ == "__main__":
    unittest.main(verbosity=2)
