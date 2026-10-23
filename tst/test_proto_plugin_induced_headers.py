"""Native h+cpp obligations follow generic protoc outputs by output parser type."""
import unittest

import lib
from test_proto_cpp import PROTOC, STYLEGUIDE, WRAPPER, pb_node, proto_files


BARE = lib.BARE_MODULE
HEADER = "$(B)/bark/base/base.pb.h"
DEEP = "$(B)/bark/deep/deep.pb.h"


def fixture(suffixes=(".facet.h",), owner_extra="", consumer_extra="", generated=None):
    files = proto_files()
    lib.tool_program(files, "tools/facet", "facet_driver")
    for module, source, content, peers in (
        ("bark/deep", "deep", 'syntax = "proto3";\n', ""),
        ("bark/base", "base", 'syntax = "proto3";\nimport "bark/deep/deep.proto";\n', "bark/deep"),
        ("bark/leaf", "leaf", 'syntax = "proto3";\nimport "bark/base/base.proto";\n', "bark/base"),
    ):
        plugin = ""
        if source == "leaf":
            name = "CPP_PROTO_PLUGIN2" if len(suffixes) == 2 else "CPP_PROTO_PLUGIN"
            plugin = f"{name}(facet tools/facet {' '.join(suffixes)})\n"
        files[f"{module}/ya.make"] = (
            "PROTO_LIBRARY()\nONLY_TAGS(CPP_PROTO)\n" + BARE
            + (f"PEERDIR({peers})\n" if peers else "") + plugin
            + (owner_extra if source == "leaf" else "")
            + f"SRCS({source}.proto)\nEND()\n"
        )
        files[f"{module}/{source}.proto"] = content
    if generated:
        del files["bark/leaf/leaf.proto"]
        files["bark/leaf/seed.proto"] = 'import "bark/base/base.proto";\n'
        lib.tool_program(files, "tools/seed", "seed_driver")
        output_includes = " OUTPUT_INCLUDES bark/base/base.proto" if generated == "includes" else ""
        files["bark/leaf/ya.make"] = files["bark/leaf/ya.make"].replace(
            "SRCS(leaf.proto)", f"RUN_PROGRAM(tools/seed IN_NOPARSE seed.proto OUT_NOAUTO leaf.proto{output_includes})\nSRCS(leaf.proto)"
        )
    files["birch/reader/ya.make"] = f"LIBRARY()\n{BARE}PEERDIR(bark/leaf)\n{consumer_extra}SRCS(read.cpp)\nEND()\n"
    files["birch/reader/read.cpp"] = f'#include "bark/leaf/leaf{suffixes[0]}"\n'
    return files


def args(node):
    return node["cmds"][0]["cmd_args"]


def dependency_outputs(graph, node):
    index = {n["uid"]: tuple(n["outputs"]) for n in graph["graph"]}
    return [index[uid] for uid in node["deps"]]


class ProtoPluginInducedHeadersTest(unittest.TestCase):
    def assert_obligations(self, graph, suffixes, consumer="$(B)/birch/reader/read.cpp.o", imported=True):
        cc = lib.node_by_output(graph, consumer)
        leaf = pb_node(graph, "$(B)/bark/leaf/leaf.pb.h")
        self.assertEqual(leaf["outputs"], [
            "$(B)/bark/leaf/leaf.pb.h", "$(B)/bark/leaf/leaf.pb.cc",
            *["$(B)/bark/leaf/leaf" + suffix for suffix in suffixes],
        ])
        self.assertEqual(args(leaf)[args(leaf).index("--outputs") + 1:args(leaf).index("--")], leaf["outputs"])
        self.assertIn(leaf["uid"], cc["deps"])
        self.assertIn(leaf["outputs"][0], cc["inputs"])
        for path in (HEADER, DEEP):
            if imported:
                producer = pb_node(graph, path)
                self.assertIn(path, cc["inputs"])
                self.assertIn(producer["uid"], cc["deps"])
                self.assertIn(tuple(producer["outputs"]), dependency_outputs(graph, cc))
            else:
                self.assertNotIn(path, cc["inputs"])
        self.assertNotIn(WRAPPER, cc["inputs"])
        self.assertNotIn("$(S)/bark/leaf/leaf.proto", cc["inputs"])
        for tool in (PROTOC, STYLEGUIDE, "$(B)/tools/facet/facet_driver"):
            self.assertIn(tool, leaf["inputs"])
            self.assertIn(lib.node_by_output(graph, tool)["uid"], leaf["deps"])
        module = consumer.removeprefix("$(B)/").rsplit("/", 1)[0]
        archive = lib.node_by_output(graph, "$(B)/" + module + "/lib" + module.replace("/", "-") + ".a")
        self.assertIn(cc["uid"], archive["deps"])
        self.assertIn(cc["outputs"][0], archive["inputs"])
        return cc

    def test_header_only_materializes_imported_and_transitive_pb_producers(self):
        graph = lib.make(fixture(), "birch/reader")
        cc = self.assert_obligations(graph, (".facet.h",))
        # The producer's own pb.h is the real main output requirement, not an
        # invented parsed include or a reason to add generated source siblings.
        self.assertNotIn("$(B)/bark/leaf/leaf.pb.cc", cc["inputs"])
        self.assertNotIn("$(B)/bark/base/base.pb.cc", cc["inputs"])

    def test_exact_input_multiset_and_dependency_identities_use_real_import_owner(self):
        from collections import Counter
        files = fixture()
        files["birch/reader/ya.make"] = files["birch/reader/ya.make"].replace("SRCS(read.cpp)", "SRCS(read.cpp standard.cpp)")
        files["birch/reader/standard.cpp"] = '#include "bark/base/base.pb.h"\n'
        graph = lib.make(files, "birch/reader")
        custom = self.assert_obligations(graph, (".facet.h",))
        standard = lib.node_by_output(graph, "$(B)/birch/reader/standard.cpp.o")
        expected = Counter(standard["inputs"])
        expected.subtract(["$(S)/birch/reader/standard.cpp"])
        expected.update(["$(S)/birch/reader/read.cpp", "$(B)/bark/leaf/leaf.facet.h", "$(B)/bark/leaf/leaf.pb.h"])
        self.assertEqual(Counter(custom["inputs"]), +expected)
        leaf = pb_node(graph, "$(B)/bark/leaf/leaf.pb.h")
        expected_deps = Counter(dependency_outputs(graph, standard))
        expected_deps.update([tuple(leaf["outputs"])])
        self.assertEqual(Counter(dependency_outputs(graph, custom)), expected_deps)

    def test_two_outputs_apply_obligations_to_generated_cpp_and_real_reader(self):
        suffixes = (".facet.h", ".facet.cpp")
        graph = lib.make(fixture(suffixes), "birch/reader", "bark/leaf")
        self.assert_obligations(graph, suffixes)
        self.assert_obligations(graph, suffixes, "$(B)/bark/leaf/leaf.facet.cpp.o")
        plugin_object = lib.node_by_output(graph, "$(B)/bark/leaf/leaf.facet.cpp.o")
        archive = lib.node_by_output(graph, "$(B)/bark/leaf/libbark-leaf.a")
        self.assertIn(plugin_object["outputs"][0], archive["inputs"])
        self.assertIn(plugin_object["uid"], archive["deps"])

    def test_output_parser_domains_select_only_native_h_and_cpp_rules(self):
        for suffix, induced in ((".facet.hpp", True), (".facet.cuh", True), (".facet.cc", True),
                                (".facet.inc", False), (".facet.txt", False), (".facet.py", False),
                                (".facet.S", False), (".facet.cu", False), (".facet.inl", False)):
            with self.subTest(suffix=suffix):
                self.assert_obligations(lib.make(fixture((suffix,)), "birch/reader"), (suffix,), imported=induced)

    def test_tool_header_cpp_and_common_domains_remain_distinct(self):
        files = fixture((".facet.h", ".facet.cpp"))
        files["tools/facet/ya.make"] = files["tools/facet/ya.make"].replace("END()", "INDUCED_DEPS(h rt/header.h)\nINDUCED_DEPS(cpp rt/cpp.h)\nINDUCED_DEPS(h+cpp rt/common.h)\nEND()")
        for name in ("header", "cpp", "common"):
            files[f"rt/{name}.h"] = "#pragma once\n"
        graph = lib.make(files, "birch/reader", "bark/leaf")
        reader = self.assert_obligations(graph, (".facet.h", ".facet.cpp"))
        cpp = self.assert_obligations(graph, (".facet.h", ".facet.cpp"), "$(B)/bark/leaf/leaf.facet.cpp.o")
        for node, wanted, unwanted in ((reader, "header", "cpp"), (cpp, "cpp", "header")):
            self.assertIn(f"$(S)/rt/{wanted}.h", node["inputs"])
            self.assertIn("$(S)/rt/common.h", node["inputs"])
            self.assertNotIn(f"$(S)/rt/{unwanted}.h", node["inputs"])

    def test_declared_generated_proto_imports_and_opaque_seed_input(self):
        for generated in ("includes", "text"):
            with self.subTest(generated=generated):
                graph = lib.make(fixture(generated=generated), "birch/reader")
                self.assert_obligations(graph, (".facet.h",), imported=generated == "includes")
                seed = lib.node_by_output(graph, "$(B)/bark/leaf/leaf.proto")
                pb = pb_node(graph, "$(B)/bark/leaf/leaf.pb.h")
                self.assertIn(seed["uid"], pb["deps"])
                self.assertIn("$(B)/bark/leaf/leaf.proto", pb["inputs"])
                self.assertIn("$(S)/bark/leaf/seed.proto", seed["inputs"])
                self.assertNotIn("$(S)/bark/base/base.proto", seed["inputs"])

    def test_each_source_keeps_its_own_import_facts(self):
        files = fixture()
        files["bark/leaf/ya.make"] = files["bark/leaf/ya.make"].replace("PEERDIR(bark/base)", "PEERDIR(bark/base bark/other)").replace("SRCS(leaf.proto)", "SRCS(leaf.proto second.proto)")
        files["bark/leaf/second.proto"] = 'import "bark/other/other.proto";\n'
        files["bark/other/ya.make"] = "PROTO_LIBRARY()\nONLY_TAGS(CPP_PROTO)\nSRCS(other.proto)\nEND()\n"
        files["bark/other/other.proto"] = 'syntax = "proto3";\n'
        files["birch/reader/ya.make"] = files["birch/reader/ya.make"].replace("SRCS(read.cpp)", "SRCS(read.cpp second.cpp)")
        files["birch/reader/second.cpp"] = '#include "bark/leaf/second.facet.h"\n'
        graph = lib.make(files, "birch/reader")
        first = self.assert_obligations(graph, (".facet.h",))
        second = lib.node_by_output(graph, "$(B)/birch/reader/second.cpp.o")
        self.assertNotIn("$(B)/bark/other/other.pb.h", first["inputs"])
        self.assertIn("$(B)/bark/other/other.pb.h", second["inputs"])
        self.assertNotIn(HEADER, second["inputs"])
        self.assertNotIn(DEEP, second["inputs"])
        other = pb_node(graph, "$(B)/bark/other/other.pb.h")
        self.assertIn(other["uid"], second["deps"])

    def test_namespace_and_explicit_source_inputs_keep_their_roots(self):
        files = fixture(owner_extra="PROTO_NAMESPACE(WITH_GEN bark/leaf)\n")
        self.assert_obligations(lib.make(files, "birch/reader"), (".facet.h",))
        files = fixture()
        files["bark/base/ya.make"] = files["bark/base/ya.make"].replace("SRCS(base.proto)", "PROTO_NAMESPACE(WITH_GEN bark/base)\nSRCS(base.proto)")
        files["bark/leaf/leaf.proto"] = 'import "base.proto";\n'
        self.assert_obligations(lib.make(files, "birch/reader"), (".facet.h",))
        files = fixture()
        files["bark/leaf/leaf.facet.h"] = '#include "physical.h"\n'
        files["bark/leaf/physical.h"] = '#pragma once\n'
        lib.tool_program(files, "tools/source_reader", "source_reader")
        files["birch/reader/read.cpp"] = "int real_source;\n"
        files["birch/reader/ya.make"] = files["birch/reader/ya.make"].replace("SRCS(read.cpp)", "RUN_PROGRAM(tools/source_reader IN ${ARCADIA_ROOT}/bark/leaf/leaf.facet.h OUT marker.cpp)\nSRCS(read.cpp)")
        graph = lib.make(files, "birch/reader")
        source_reader = lib.node_by_output(graph, "$(B)/birch/reader/marker.cpp")
        self.assertIn("$(S)/bark/leaf/leaf.facet.h", source_reader["inputs"])
        self.assertIn("$(S)/bark/leaf/physical.h", source_reader["inputs"])
        self.assertNotIn("$(B)/bark/leaf/leaf.facet.h", source_reader["inputs"])
        self.assertNotIn(HEADER, source_reader["inputs"])
        self.assertNotIn(DEEP, source_reader["inputs"])

    def test_resolution_contexts_and_both_root_orders(self):
        for roots in (("birch/reader", "bark/base"), ("bark/base", "birch/reader")):
            with self.subTest(roots=roots):
                self.assert_obligations(lib.make(fixture(), roots[0], roots[1]), (".facet.h",))
        self.assert_obligations(lib.make(fixture(owner_extra="SET(DONT_RESOLVE_INCLUDES yes)\n"), "birch/reader"), (".facet.h",))
        graph = lib.make(fixture(consumer_extra="SET(DONT_RESOLVE_INCLUDES yes)\n"), "birch/reader")
        cc = lib.node_by_output(graph, "$(B)/birch/reader/read.cpp.o")
        self.assertEqual(cc["inputs"], ["$(S)/birch/reader/read.cpp"])
        self.assertFalse(any("$(B)/bark/base/base.pb.h" in n["outputs"] for n in graph["graph"]))


if __name__ == "__main__":
    unittest.main()
