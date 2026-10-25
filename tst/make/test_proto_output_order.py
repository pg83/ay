import unittest

from tst import lib
from tst.make.test_proto_cpp import GRPC_CPP, PROTOC, STYLEGUIDE, WRAPPER, grpc_files, pb_node, proto_files


# The pinned CPP_PROTO concrete variant attaches this epilogue. Its sibling
# CPP_PROTO_FROM_SCHEMA inherits _CPP_PROTO without attaching the hook.
HEADER_EPILOGUE = """macro _CPP_PROTO_EPILOGUE() {
    ASSERT(_PROTOC_HEADERS_MODE_OK PROTOC_DIRECT_HEADERS and ALLOW_PROTOC_DIRECT_HEADERS_CONSUMERS cannot be combined)
    ASSERT(_PROTOC_DIRECT_GENERATOR_OK PROTOC_DIRECT_HEADERS requires the default ya make C++ generator without extra protoc plugins; CMake export is not supported)
    SET(PROTOC_TRANSITIVE_HEADERS $_CPP_PROTO_HEADERS_TRANSITIVE)
    SET(MODULE_TAG $_CPP_PROTO_HEADERS_TAG)
    SET(PEERDIR_TAGS $_CPP_PROTO_HEADERS_PEERS)
    SET(CHECK_IMPLICIT_PEERDIR_TAGS $_CPP_PROTO_HEADERS_CHECK_IMPLICIT_TAGS)
}
"""
PLUGIN = "CPP_PROTO_PLUGIN(mark tools/ornament .mark.h)\n"
PAIR = "CPP_PROTO_PLUGIN2(pair tools/paired .pair.h .pair.cc)\n"
H, CC, MARK, PAIR_H, PAIR_CC = ".pb.h", ".pb.cc", ".mark.h", ".pair.h", ".pair.cc"
GRPC_CC, GRPC_H, DEPS = ".grpc.pb.cc", ".grpc.pb.h", ".deps.pb.h"


def order_files(kind, body, epilogue=True):
    files = proto_files()
    grpc_files(files)
    lib.tool_program(files, "tools/ornament", "ornament_driver")
    lib.tool_program(files, "tools/paired", "paired_driver")
    if epilogue:
        files["build/conf/proto.conf"] = HEADER_EPILOGUE
    tag = "ONLY_TAGS(CPP_PROTO)\n" if kind == "PROTO_LIBRARY" else ""
    files.update({
        "cedar/ya.make": (
            f"{kind}()\n{tag}{lib.BARE_MODULE}INCLUDE(order.inc)\n"
            "SRCS(note.proto base.proto)\nEND()\n"
        ),
        "cedar/order.inc": body,
        "cedar/note.proto": 'syntax = "proto3";\nimport "cedar/base.proto";\nmessage Note {}\n',
        "cedar/base.proto": 'syntax = "proto3";\nmessage Base {}\n',
        "reader/ya.make": f"PROGRAM()\n{lib.BARE_MODULE}PEERDIR(cedar)\nSRCS(read.cpp)\nEND()\n",
        "reader/read.cpp": '#include <cedar/note.mark.h>\nint main(){return 0;}\n',
    })
    return files


class ProtoOutputOrderTest(unittest.TestCase):
    def check_order(self, graph, suffixes):
        node = pb_node(graph, "$(B)/cedar/note.pb.h")
        outputs = ["$(B)/cedar/note" + suffix for suffix in suffixes]
        self.assertEqual(node["outputs"], outputs)
        args = node["cmds"][0]["cmd_args"]
        self.assertEqual(args[args.index("--outputs") + 1:args.index("--")], outputs)
        self.assertEqual(node["cmds"][0]["cwd"], "$(S)")
        for source in (WRAPPER, "$(S)/cedar/note.proto", "$(S)/cedar/base.proto"):
            self.assertIn(source, node["inputs"])
        tools = [PROTOC, STYLEGUIDE, "$(B)/tools/ornament/ornament_driver"]
        if PAIR_H in suffixes:
            tools.append("$(B)/tools/paired/paired_driver")
        if GRPC_H in suffixes:
            tools.append(GRPC_CPP)
        for tool in tools:
            self.assertIn(tool, node["inputs"])
            self.assertIn(lib.node_by_output(graph, tool)["uid"], node["deps"])
        for output in ("$(B)/cedar/note.pb.cc.o", "$(B)/reader/read.cpp.o"):
            consumer = lib.node_by_output(graph, output)
            self.assertIn(node["uid"], consumer["deps"])
            self.assertIn(outputs[0], consumer["inputs"])
            self.assertNotIn(WRAPPER, consumer["inputs"])
        reader = lib.node_by_output(graph, "$(B)/reader/read.cpp.o")
        self.assertIn("$(B)/cedar/note.mark.h", reader["inputs"])
        if PAIR_CC in suffixes:
            compiled = lib.node_by_output(graph, "$(B)/cedar/note.pair.cc.o")
            self.assertIn(node["uid"], compiled["deps"])
            self.assertIn(outputs[0], compiled["inputs"])

    def test_only_the_proto_library_cpp_variant_runs_the_header_epilogue(self):
        for epilogue in (False, True):
            for kind in ("PROTO_SCHEMA", "PROTO_LIBRARY", "LIBRARY"):
                with self.subTest(epilogue=epilogue, kind=kind):
                    graph = lib.make(order_files(kind, PLUGIN + PAIR, epilogue), "reader", "--sandboxing")
                    suffixes = [H, CC, MARK, PAIR_H, PAIR_CC]
                    if epilogue and kind == "PROTO_LIBRARY":
                        suffixes = [H, MARK, PAIR_H, PAIR_CC, CC]
                    self.check_order(graph, suffixes)

    def test_explicit_header_assignments_keep_source_order_without_a_schema_epilogue(self):
        cases = (
            (PLUGIN + "SET(PROTOC_TRANSITIVE_HEADERS yes)\n" + PAIR,
             [H, MARK, CC, PAIR_H, PAIR_CC], [H, MARK, PAIR_H, PAIR_CC, CC]),
            (PLUGIN + "SET(PROTOC_TRANSITIVE_HEADERS no)\n" + PAIR,
             [H, MARK, CC, DEPS, PAIR_H, PAIR_CC], [H, MARK, PAIR_H, PAIR_CC, CC, DEPS]),
            ("SET(PROTOC_TRANSITIVE_HEADERS no)\n" + PLUGIN + PAIR,
             [H, CC, DEPS, MARK, PAIR_H, PAIR_CC], [H, MARK, PAIR_H, PAIR_CC, CC, DEPS]),
            (PLUGIN + "SET(PROTOC_TRANSITIVE_HEADERS no)\n" + PAIR + "SET(PROTOC_TRANSITIVE_HEADERS yes)\n",
             [H, MARK, PAIR_H, PAIR_CC, CC], [H, MARK, PAIR_H, PAIR_CC, CC]),
        )
        for kind in ("PROTO_SCHEMA", "PROTO_LIBRARY", "LIBRARY"):
            for body, source_order, epilogue_order in cases:
                with self.subTest(kind=kind, body=body):
                    graph = lib.make(order_files(kind, body), "reader", "--sandboxing")
                    self.check_order(graph, epilogue_order if kind == "PROTO_LIBRARY" else source_order)

    def test_grpc_and_plugin2_keep_their_declaration_positions(self):
        cases = (
            ("GRPC()\n" + PLUGIN, [H, CC, GRPC_CC, GRPC_H, MARK], [H, GRPC_CC, GRPC_H, MARK, CC]),
            (PLUGIN + "GRPC()\n", [H, CC, MARK, GRPC_CC, GRPC_H], [H, MARK, GRPC_CC, GRPC_H, CC]),
            ("GRPC()\n" + PLUGIN + "SET(PROTOC_TRANSITIVE_HEADERS no)\n" + PAIR,
             [H, GRPC_CC, GRPC_H, MARK, CC, DEPS, PAIR_H, PAIR_CC],
             [H, GRPC_CC, GRPC_H, MARK, PAIR_H, PAIR_CC, CC, DEPS]),
        )
        for kind in ("PROTO_SCHEMA", "PROTO_LIBRARY", "LIBRARY"):
            for body, source_order, epilogue_order in cases:
                with self.subTest(kind=kind, body=body):
                    graph = lib.make(order_files(kind, body), "reader", "--sandboxing")
                    self.check_order(graph, epilogue_order if kind == "PROTO_LIBRARY" else source_order)

    def test_shared_lexical_include_keeps_each_concrete_variant_hook(self):
        files = order_files("PROTO_SCHEMA", PLUGIN)
        files["cedar/ya.make"] = files["cedar/ya.make"].replace("INCLUDE(order.inc)", "INCLUDE(${ARCADIA_ROOT}/shared/order.inc)")
        files["shared/order.inc"] = files.pop("cedar/order.inc")
        files["spruce/ya.make"] = files["cedar/ya.make"].replace("PROTO_SCHEMA()", "PROTO_LIBRARY()\nONLY_TAGS(CPP_PROTO)")
        files["spruce/note.proto"] = 'syntax = "proto3";\nmessage Note {}\n'
        files["spruce/base.proto"] = 'syntax = "proto3";\nmessage Base {}\n'
        files["reader/ya.make"] = files["reader/ya.make"].replace("PEERDIR(cedar)", "PEERDIR(cedar spruce)")
        graph = lib.make(files, "reader", "--sandboxing")
        self.check_order(graph, [H, CC, MARK])
        self.assertEqual(pb_node(graph, "$(B)/spruce/note.pb.h")["outputs"], [
            "$(B)/spruce/note.pb.h", "$(B)/spruce/note.mark.h", "$(B)/spruce/note.pb.cc",
        ])


if __name__ == "__main__":
    unittest.main(verbosity=2)
