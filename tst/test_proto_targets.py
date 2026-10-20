import unittest

import lib
from test_go_package import STD, go_module, go_world
from test_proto_cpp import proto_files
from test_proto_desc import desc_files
from test_proto_py_variant import py_proto_files
from test_java import contrib


def target_files():
    files = {**lib.java_tree(), **py_proto_files(), **go_world(), **desc_files(), **proto_files()}
    files.update(contrib("contrib/java/com/google/protobuf/protobuf-java/4.33.0", jar=7))
    for peer in (
        *[f"{STD}/{name}" for name in ("sync", "reflect", "fmt", "math")],
        "vendor/github.com/golang/protobuf/proto",
        *[f"vendor/google.golang.org/protobuf/{name}" for name in (
            "types/descriptorpb", "runtime/protoimpl", "runtime/protoiface", "reflect/protoreflect",
            *[f"types/known/{name}pb" for name in (
                "any", "api", "duration", "empty", "fieldmask", "sourcecontext", "struct",
                "timestamp", "type", "wrappers")],
        )],
    ):
        go_module(files, peer)
    lib.tool_program(files, "vendor/github.com/golang/protobuf/protoc-gen-go", "protoc-gen-go")
    files["p/ya.make"] = "PROTO_LIBRARY()\nSRCS(a.proto)\nEND()\n"
    files["p/a.proto"] = 'syntax = "proto3";\nmessage A {}\n'
    return files


class ProtoTargetsTest(unittest.TestCase):
    def test_secondary_generated_proto_keeps_main_output_in_inputs(self):
        files = target_files()
        lib.tool_program(files, "gen", "gen")
        files["p/ya.make"] = (
            "PROTO_LIBRARY()\nRUN_PROGRAM(gen OUT_NOAUTO main.proto extra.proto)\n"
            "SRCS(extra.proto)\nEND()\n"
        )
        graph = lib.make(files, "p", "--sandboxing")
        producer = lib.node_by_output(graph, "$(B)/p/main.proto")
        for output in ("extra.pb.h", "extra.pb.go", "extra__intpy3___pb2.py"):
            with self.subTest(output=output):
                generated = lib.node_by_output(graph, "$(B)/p/" + output)
                self.assertIn("$(B)/p/main.proto", generated["inputs"])
                self.assertEqual(generated["deps"].count(producer["uid"]), 1)
                self.assertNotIn(producer["uid"], generated.get("foreign_deps", {}).get("tool", []))

    def test_default_start_variants(self):
        graph = lib.make(target_files(), "p")
        for output in ("$(B)/p/a.pb.cc", "$(B)/p/a.pb.go", "$(B)/p/a__intpy3___pb2.py"):
            lib.node_by_output(graph, output)

    def test_only_and_include_tags_select_start_variants(self):
        files = target_files()
        files["p/ya.make"] = (
            "PROTO_LIBRARY()\nONLY_TAGS(GO_PROTO UNRELATED_TAG)\nINCLUDE_TAGS(DESC_PROTO)\n"
            "SRCS(a.proto)\nEND()\n")
        graph = lib.make(files, "p")
        lib.node_by_output(graph, "$(B)/p/a.pb.go")
        lib.node_by_output(graph, "$(B)/p/p.self.protodesc")
        self.assertFalse(any("$(B)/p/a.pb.cc" in n["outputs"] for n in graph["graph"]))

    def test_python_proto_library_keeps_explicit_resource_objects(self):
        files = target_files()
        files["p/ya.make"] = "PROTO_LIBRARY()\nONLY_TAGS(CPP_PROTO PY3_PROTO)\nSRCS(a.proto)\nRESOURCE_FILES(data.txt)\nEND()\n"
        files["p/data.txt"] = "data\n"
        graph = lib.make(files, "p")
        archive = lib.node_by_output(graph, "$(B)/p/libpy3p.global.a")
        resources = [n for n in graph["graph"] if "$(S)/p/data.txt" in n["inputs"] and n["kv"]["p"] == "PY"]
        self.assertTrue(resources)
        self.assertTrue(any(n["uid"] in archive["deps"] for n in resources))

    def test_conditional_module_declaration_keeps_start_variants(self):
        files = target_files()
        files["p/ya.make"] = (
            "SET(ENABLED yes)\nIF (ENABLED)\nINCLUDE(module.inc)\nENDIF()\n"
        )
        files["p/module.inc"] = "PROTO_LIBRARY()\nSRCS(a.proto)\nEND()\n"
        graph = lib.make(files, "p")
        for output in ("$(B)/p/p.jar", "$(B)/p/a.pb.go", "$(B)/p/a.pb.cc"):
            lib.node_by_output(graph, output)

    def test_opt_in_docs_peers_supply_include_paths(self):
        files = target_files()
        lib.tool_program(files, "vendor/github.com/pseudomuto/protoc-gen-doc/cmd/protoc-gen-doc", "protoc-gen-doc")
        files["p/ya.make"] = (
            "PROTO_LIBRARY()\nONLY_TAGS(JAVA_PROTO)\nINCLUDE_TAGS(DOCS_PROTO)\n"
            "PROTO_NAMESPACE(p)\nPEERDIR(q)\nSRCS(a.proto)\nEND()\n"
        )
        files["q/ya.make"] = (
            "PROTO_LIBRARY()\nINCLUDE_TAGS(DOCS_PROTO)\nPROTO_NAMESPACE(GLOBAL q)\nSRCS(a/same.proto b/same.proto)\nEND()\n"
        )
        files["q/a/same.proto"] = files["q/b/same.proto"] = 'syntax = "proto3";\n'
        graph = lib.make(files, "p")
        doc = lib.node_by_output(graph, "$(B)/p/a.md")
        self.assertEqual(doc["kv"], {"p": "PD", "pc": "yellow"})
        self.assertIn("-I=$(S)/q", doc["cmds"][0]["cmd_args"])
        self.assertIn(doc["uid"], lib.node_by_output(graph, "$(B)/p/p.docslib")["deps"])
        self.assertFalse(any("$(B)/p/a.pb.cc" in n["outputs"] for n in graph["graph"]))


if __name__ == "__main__":
    unittest.main(verbosity=2)
