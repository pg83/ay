import json
import unittest

from tst import lib
from tst.make.test_java import contrib
from tst.make.test_proto_cpp import proto_files
from tst.make.test_proto_desc import desc_files
from tst.make.test_proto_py2_context import proto_world


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


def python_files(shadow=True, token="catalog.proto", later=False):
    sources = proto_world()
    lib.tool_program(sources, "tools/carver", "carver-binary")
    sources["py3/ya.make"] = (
        f"PY3_PROGRAM()\n{lib.BARE_MODULE}NO_CHECK_IMPORTS()\nPEERDIR(ochre/schemas)\nEND()\n"
    )
    declaration = ("RUN_PROGRAM(tools/carver OUT_NOAUTO ${BINDIR}/catalog.proto "
                   "OUTPUT_INCLUDES ochre/imports/generated.proto)\n")
    srcs = f"SRCS({token})\n"
    sources["ochre/schemas/ya.make"] = (
        f"PROTO_LIBRARY()\n{lib.BARE_MODULE}NO_MYPY()\nNO_OPTIMIZE_PY_PROTOS()\n"
        + (srcs + declaration if later else declaration + srcs) + "END()\n"
    )
    sources["ochre/imports/generated.proto"] = 'syntax = "proto3";\n'
    sources["ochre/imports/source.proto"] = 'syntax = "proto3";\n'
    if shadow:
        sources["ochre/schemas/catalog.proto"] = (
            'syntax = "proto3";\nimport "ochre/imports/source.proto";\n'
        )
    return sources


class ProtoDescGeneratedTest(unittest.TestCase):
    def test_python_proto_prefers_source_and_retains_selected_imports(self):
        for generated in (False, True):
            with self.subTest(generated=generated):
                sources = python_files(shadow=not generated)
                graph = lib.make(sources, "py3")
                pb = lib.node_by_output(graph, "$(B)/ochre/schemas/catalog__intpy3___pb2.py")
                root = "$(B)" if generated else "$(S)"
                self.assertIn(root + "/ochre/schemas/catalog.proto", pb["inputs"])
                self.assertEqual(pb["cmds"][0]["cwd"], root)
                chosen = "generated" if generated else "source"
                other = "source" if generated else "generated"
                self.assertIn(f"$(S)/ochre/imports/{chosen}.proto", pb["inputs"])
                self.assertNotIn(f"$(S)/ochre/imports/{other}.proto", pb["inputs"])
                if generated:
                    producer = lib.node_by_output(graph, "$(B)/ochre/schemas/catalog.proto")
                    self.assertIn(producer["uid"], pb["deps"])
                    tool = lib.node_by_output(graph, "$(B)/tools/carver/carver-binary")
                    self.assertIn(tool["uid"], producer["deps"])
                else:
                    self.assertFalse(any("$(B)/ochre/schemas/catalog.proto" in n["outputs"]
                                         for n in graph["graph"]))

    def test_python_explicit_namespaces_and_later_producer(self):
        for token, root in (("${ARCADIA_ROOT}/ochre/schemas/catalog.proto", "$(S)"),
                            ('"$(S)/ochre/schemas/catalog.proto"', "$(S)"),
                            ("${BINDIR}/catalog.proto", "$(B)"),
                            ('"$(B)/ochre/schemas/catalog.proto"', "$(B)")):
            with self.subTest(token=token):
                graph = lib.make(python_files(token=token, later=True), "py3")
                node = lib.node_by_output(graph, "$(B)/ochre/schemas/catalog__intpy3___pb2.py")
                self.assertIn(root + "/ochre/schemas/catalog.proto", node["inputs"])
                self.assertEqual(node["cmds"][0]["cwd"], root)
                chosen, other = (("generated", "source") if root == "$(B)" else ("source", "generated"))
                self.assertIn(f"$(S)/ochre/imports/{chosen}.proto", node["inputs"])
                self.assertNotIn(f"$(S)/ochre/imports/{other}.proto", node["inputs"])
                if root == "$(B)":
                    producer = lib.node_by_output(graph, "$(B)/ochre/schemas/catalog.proto")
                    self.assertIn(producer["uid"], node["deps"])

    def test_gazetteer_cpp_and_python_keep_native_shadow_namespaces(self):
        for token, srcdir, resolved in (("g.gztproto", "", "p/g"),
                                       ("proto/g.gztproto", "", "p/proto/g"),
                                       ("g.gztproto", "SRCDIR(ochre/gzt_src)\n", "ochre/gzt_src/g")):
            with self.subTest(token=token, srcdir=srcdir):
                sources = proto_world()
                sources["p/ya.make"] = (
                    f"PROTO_LIBRARY()\nONLY_TAGS(CPP_PROTO PY3_PROTO)\n{lib.BARE_MODULE}"
                    f"NO_MYPY()\n{srcdir}SRCS({token})\nEND()\n"
                )
                sources["py3/ya.make"] = f"PY3_PROGRAM()\n{lib.BARE_MODULE}NO_CHECK_IMPORTS()\nPEERDIR(p)\nEND()\n"
                sources["build/platform/python/ymake_python3/ya.make"] = lib.resource_lib("YMAKE_PYTHON3", 861)
                sources[resolved + ".gztproto"] = "message Generated {}\n"
                sources[resolved + ".proto"] = 'syntax = "proto3";\nimport "q/b.proto";\n'
                graph = lib.make(sources, "py3", "p")
                converter = lib.node_by_output(graph, "$(B)/" + resolved + ".proto")
                self.assertIn("$(S)/" + resolved + ".gztproto", converter["inputs"])
                cpp = lib.node_by_output(graph, "$(B)/" + resolved + ".pb.h")
                self.assertIn("$(B)/" + resolved + ".proto", cpp["inputs"])
                self.assertNotIn("$(S)/" + resolved + ".proto", cpp["inputs"])
                self.assertNotIn("$(S)/q/b.proto", cpp["inputs"])
                self.assertIn(converter["uid"], cpp["deps"])
                python = lib.node_by_output(graph, "$(B)/" + resolved + "__intpy3___pb2.py")
                self.assertIn("$(S)/" + resolved + ".proto", python["inputs"])
                self.assertIn("$(S)/q/b.proto", python["inputs"])
                self.assertNotIn("$(B)/" + resolved + ".proto", python["inputs"])
                self.assertNotIn(converter["uid"], python["deps"])
                self.assertEqual(python["cmds"][0]["cwd"], "$(S)")

    def test_srcdir_source_precedes_the_registered_shadow(self):
        sources = files(tags="CPP_PROTO JAVA_PROTO DESC_PROTO")
        sources["ochre/schema/schema.proto"] = 'syntax = "proto3";\n'
        sources["p/ya.make"] = sources["p/ya.make"].replace(
            "${BINDIR}/schema.proto", "${ARCADIA_BUILD_ROOT}/ochre/schema/schema.proto").replace(
            "SRCS(schema.proto)", "SRCDIR(ochre/schema)\nSRCS(schema.proto)")
        graph = lib.make(sources, "p")
        for out in ("$(B)/ochre/schema/schema.pb.h", "$(B)/ochre/schema/schema.jsrc", "$(B)/p/__/ochre/schema/schema.proto.desc"):
            node = lib.node_by_output(graph, out)
            self.assertIn("$(S)/ochre/schema/schema.proto", node["inputs"])
            self.assertNotIn("$(B)/ochre/schema/schema.proto", node["inputs"])
            self.assertNotIn("$(S)/types/dependency.proto", node["inputs"])
            self.assertEqual(node["cmds"][0]["cwd"], "$(S)")

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

    def test_cpp_proto_uses_physical_source_before_same_named_generator(self):
        graph = lib.make(files(tags="CPP_PROTO", shadow=True), "p")
        cpp = lib.node_by_output(graph, "$(B)/p/schema.pb.h")
        self.assertIn("$(S)/p/schema.proto", cpp["inputs"])
        self.assertNotIn("$(B)/p/schema.proto", cpp["inputs"])
        self.assertNotIn("$(S)/types/dependency.proto", cpp["inputs"])
        self.assertEqual(cpp["cmds"][0]["cwd"], "$(S)")

    def test_source_proto_does_not_create_a_scheduler_cycle_with_unused_shadow(self):
        sources = files(tags="CPP_PROTO", shadow=True)
        sources["p/ya.make"] = sources["p/ya.make"].replace(
            "RUN_PROGRAM(tools/generate OUT_NOAUTO", "RUN_PROGRAM(tools/generate IN ${BINDIR}/schema.pb.h OUT_NOAUTO")
        graph = lib.make(sources, "p")
        cpp = lib.node_by_output(graph, "$(B)/p/schema.pb.h")
        self.assertIn("$(S)/p/schema.proto", cpp["inputs"])
        self.assertFalse(any("$(B)/p/schema.proto" in node["outputs"] for node in graph["graph"]))

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
        for token, root in (("${ARCADIA_ROOT}/p/schema.proto", "$(S)"),
                            ("${CURDIR}/schema.proto", "$(S)"),
                            ('"$(S)/p/schema.proto"', "$(S)"),
                            ("${BINDIR}/schema.proto", "$(B)"),
                            ("${ARCADIA_BUILD_ROOT}/p/schema.proto", "$(B)"),
                            ('"$(B)/p/schema.proto"', "$(B)")):
            with self.subTest(token=token):
                sources = files(tags="CPP_PROTO JAVA_PROTO DESC_PROTO", shadow=True)
                sources["p/ya.make"] = sources["p/ya.make"].replace("SRCS(schema.proto)", f"SRCS({token})")
                graph = lib.make(sources, "p")
                for output in ("$(B)/p/schema.proto.desc", "$(B)/p/schema.jsrc", "$(B)/p/schema.pb.h"):
                    node = lib.node_by_output(graph, output)
                    self.assertIn(root + "/p/schema.proto", node["inputs"])
                    self.assertEqual(node["cmds"][0]["cwd"], root)

    def test_explicit_source_namespace_does_not_bind_a_build_producer_when_absent(self):
        sources = files(tags="CPP_PROTO")
        sources["p/ya.make"] = sources["p/ya.make"].replace(
            "SRCS(schema.proto)", "SRCS(${ARCADIA_ROOT}/p/schema.proto)")
        result = lib.make_process(sources, "p")
        graph = json.loads(result.stdout)
        node = lib.node_by_output(graph, "$(B)/p/schema.pb.h")
        self.assertIn("$(S)/p/schema.proto", node["inputs"])
        self.assertNotIn("$(B)/p/schema.proto", node["inputs"])
        self.assertFalse(any("$(B)/p/schema.proto" in n["outputs"] for n in graph["graph"]))


if __name__ == "__main__":
    unittest.main(verbosity=2)
