import unittest

import lib
from test_java import contrib
from test_proto_cpp import proto_files
from test_proto_desc import desc_files


def files(tags="CPP_PROTO DESC_PROTO", shadow=False):
    sources = proto_files()
    sources.update(desc_files())
    lib.tool_program(sources, "contrib/tools/protoc/plugins/cpp_styleguide", "cpp_styleguide")
    lib.tool_program(sources, "tools/generate", "generate")
    sources.update({
        "p/ya.make": f"PROTO_LIBRARY()\nONLY_TAGS({tags})\n"
            "IF(GEN_PROTO)\nRUN_PROGRAM(tools/generate OUT_NOAUTO ${BINDIR}/schema.proto OUTPUT_INCLUDES types/dependency.proto)\nENDIF()\n"
            "SRCS(schema.proto)\nPEERDIR(types)\nEND()\n",
        "types/ya.make": "PROTO_LIBRARY()\nONLY_TAGS(CPP_PROTO DESC_PROTO)\nSRCS(dependency.proto)\nEND()\n",
        "types/dependency.proto": 'syntax = "proto3";\n',
    })
    if shadow:
        sources["p/schema.proto"] = 'syntax = "proto3";\n'
    if "JAVA_PROTO" in tags:
        sources.update(lib.java_tree())
        sources.update(contrib("contrib/java/com/google/protobuf/protobuf-java/4.33.0", jar=7))
    return sources


class ProtoDescGeneratedTest(unittest.TestCase):
    def test_descriptor_reads_the_existing_generated_proto_and_its_imports(self):
        graph = lib.make(files(), "p")
        producer = lib.node_by_output(graph, "$(B)/p/schema.proto")
        descriptor = lib.node_by_output(graph, "$(B)/p/schema.proto.desc")
        self.assertIn("$(B)/p/schema.proto", descriptor["inputs"])
        self.assertIn("$(S)/types/dependency.proto", descriptor["inputs"])
        self.assertNotIn("$(S)/p/schema.proto", descriptor["inputs"])
        self.assertEqual(descriptor["cmds"][0]["cwd"], "$(B)")
        self.assertIn(producer["uid"], descriptor["deps"])
        self.assertEqual(len([n for n in graph["graph"] if "$(B)/p/schema.proto" in n["outputs"]]), 1)

    def test_physical_proto_precedes_a_generated_proto(self):
        graph = lib.make(files(shadow=True), "p")
        descriptor = lib.node_by_output(graph, "$(B)/p/schema.proto.desc")
        self.assertIn("$(S)/p/schema.proto", descriptor["inputs"])
        self.assertNotIn("$(B)/p/schema.proto", descriptor["inputs"])
        self.assertNotIn("$(S)/types/dependency.proto", descriptor["inputs"])
        self.assertEqual(descriptor["cmds"][0]["cwd"], "$(S)")

    def test_descriptor_only_does_not_enable_a_generator(self):
        graph = lib.make(files(tags="DESC_PROTO", shadow=True), "p")
        descriptor = lib.node_by_output(graph, "$(B)/p/schema.proto.desc")
        self.assertIn("$(S)/p/schema.proto", descriptor["inputs"])
        self.assertFalse(any("$(B)/p/schema.proto" in n["outputs"] for n in graph["graph"]))

    def test_java_proto_uses_generated_input_and_imports(self):
        graph = lib.make(files(tags="CPP_PROTO JAVA_PROTO DESC_PROTO"), "p")
        producer = lib.node_by_output(graph, "$(B)/p/schema.proto")
        java = lib.node_by_output(graph, "$(B)/p/schema.jsrc")
        self.assertIn("$(B)/p/schema.proto", java["inputs"])
        self.assertIn("$(S)/types/dependency.proto", java["inputs"])
        self.assertNotIn("$(S)/p/schema.proto", java["inputs"])
        self.assertEqual(java["cmds"][0]["cwd"], "$(B)")
        self.assertIn(producer["uid"], java["deps"])

    def test_java_physical_proto_precedes_a_generated_proto(self):
        graph = lib.make(files(tags="CPP_PROTO JAVA_PROTO", shadow=True), "p")
        java = lib.node_by_output(graph, "$(B)/p/schema.jsrc")
        self.assertIn("$(S)/p/schema.proto", java["inputs"])
        self.assertNotIn("$(B)/p/schema.proto", java["inputs"])
        self.assertNotIn("$(S)/types/dependency.proto", java["inputs"])
        self.assertEqual(java["cmds"][0]["cwd"], "$(S)")

    def test_explicit_proto_roots_are_preserved(self):
        for token, root in (("${ARCADIA_ROOT}/p/schema.proto", "$(S)"), ("${BINDIR}/schema.proto", "$(B)")):
            with self.subTest(token=token):
                sources = files(tags="CPP_PROTO JAVA_PROTO DESC_PROTO", shadow=True)
                sources["p/ya.make"] = sources["p/ya.make"].replace("SRCS(schema.proto)", f"SRCS({token})")
                graph = lib.make(sources, "p")
                for output in ("$(B)/p/schema.proto.desc", "$(B)/p/schema.jsrc"):
                    node = lib.node_by_output(graph, output)
                    self.assertIn(root + "/p/schema.proto", node["inputs"])
                    self.assertEqual(node["cmds"][0]["cwd"], root)


if __name__ == "__main__":
    unittest.main(verbosity=2)
