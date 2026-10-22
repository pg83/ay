import unittest

import lib
from test_proto_py2_context import proto_world
from test_python_proto import MYPY, PROTOC, WRAPPER, args


MODULE = "contracts/cedar"
ROOT = "wire/cedar"
TOOL = "generators/letter"
TOOL_BINARY = "$(B)/generators/letter/letter"
END_INCLUDES = ["-I=$(B)", "-I=$(S)/contrib/libs/protobuf/src"]


def includes(node):
    return [arg for arg in args(node) if arg.startswith("-I=")]


def prefix(root=ROOT):
    return [f"-I=./{root}", f"-I=$(S)/{root}", "-I=$(B)", "-I=$(S)"]


def world(body=None):
    files = proto_world()
    lib.tool_program(files, TOOL, "letter")
    if body is None:
        body = f"PROTO_NAMESPACE(WITH_GEN {ROOT})\n"
    files.update({
        f"{MODULE}/ya.make": (
            f"PROTO_LIBRARY()\n{lib.BARE_MODULE}ONLY_TAGS(PY_PROTO PY3_PROTO)\n"
            f"{body}PY_NAMESPACE(public.cedar)\nPY_PROTO_PLUGIN(letter _stub.py {TOOL})\n"
            f"SRCDIR({ROOT})\nSRCS(schema/packet.proto)\nEND()\n"
        ),
        f"{ROOT}/schema/packet.proto": 'syntax = "proto2";\nimport "schema/common.proto";\nmessage Packet {}\n',
        f"{ROOT}/schema/common.proto": 'syntax = "proto2";\nmessage Common {}\n',
    })
    for project, version in (("client2", "PY2"), ("client3", "PY3")):
        files[f"{project}/ya.make"] = (
            f"{version}_PROGRAM()\n{lib.BARE_MODULE}NO_CHECK_IMPORTS()\nPEERDIR({MODULE})\nEND()\n"
        )
    return files


def producer(graph, version="py3", source="schema/packet", root=ROOT):
    return lib.node_by_output(graph, f"$(B)/{root}/{source}__int{version}___pb2.py")


class PythonProtoRootIncludesTest(unittest.TestCase):
    def test_with_gen_keeps_build_before_source_in_both_python_commands(self):
        graph = lib.make(world(), "client2", "client3")
        plugin = lib.node_by_output(graph, TOOL_BINARY)
        for version, protoc in (("py2", "$(B)/contrib/tools/protoc_old/protoc_old"), ("py3", PROTOC)):
            with self.subTest(version=version):
                node = producer(graph, version)
                self.assertEqual(args(node)[1:], [
                    WRAPPER, "--py_ver", version, "--suffixes", "_pb2.py", "_stub.py", "_pb2.pyi",
                    "--input", f"{ROOT}/schema/packet.proto", "--ns", f"/{ROOT}", "--", protoc,
                    *prefix(), f"-I=$(B)/{ROOT}", f"-I=$(S)/{ROOT}", *END_INCLUDES,
                    f"--python_out=$(B)/{ROOT}", f"{ROOT}/schema/packet.proto",
                    f"--plugin=protoc-gen-letter={TOOL_BINARY}", f"--letter_out=$(B)/{ROOT}",
                    f"--plugin=protoc-gen-mypy={MYPY}", f"--mypy_out=$(B)/{ROOT}",
                ])
                stem = f"$(B)/{ROOT}/schema/packet__int{version}__"
                self.assertEqual(node["outputs"], [stem + x for x in ("_pb2.py", "_stub.py", "_pb2.pyi")])
                self.assertEqual(node["cmds"][0]["cwd"], "$(S)")
                for path in (protoc, TOOL_BINARY, MYPY, WRAPPER,
                             f"$(S)/{ROOT}/schema/packet.proto", f"$(S)/{ROOT}/schema/common.proto"):
                    self.assertIn(path, node["inputs"])
                self.assertIn(plugin["uid"], node["foreign_deps"]["tool"])
                stub = stem + "_stub.py"
                bytecode = next(n for n in graph["graph"] if stub in args(n)
                                and n["outputs"][0].endswith(".yapyc" if version == "py2" else ".yapyc3"))
                self.assertIn(node["outputs"][0], bytecode["inputs"])
                self.assertIn(node["uid"], bytecode["deps"])

    def test_local_declaration_order_is_separate_from_peer_uniqueness(self):
        files = world(
            "ADDINCL(FOR proto manual/first GLOBAL FOR proto manual/global FOR proto manual/middle)\n"
            f"PROTO_NAMESPACE(WITH_GEN {ROOT})\nPROTO_ADDINCL(WITH_GEN local/extra)\n"
            "ADDINCL(FOR proto manual/last)\nPEERDIR(support/same)\n"
        )
        files["support/same/ya.make"] = (
            f"PROTO_LIBRARY()\n{lib.BARE_MODULE}ONLY_TAGS(PY_PROTO PY3_PROTO)\n"
            f"PROTO_NAMESPACE(WITH_GEN {ROOT})\n"
            "PROTO_ADDINCL(GLOBAL WITH_GEN propagated/extra)\nEND()\n"
        )
        graph = lib.make(files, "client3")
        self.assertEqual(includes(producer(graph)), [
            *prefix(), "-I=$(S)/manual/first", "-I=$(S)/manual/global", "-I=$(S)/manual/middle",
            f"-I=$(B)/{ROOT}", f"-I=$(S)/{ROOT}", "-I=$(B)/local/extra", "-I=$(S)/local/extra",
            "-I=$(S)/manual/last", f"-I=$(B)/{ROOT}", f"-I=$(S)/{ROOT}",
            "-I=$(B)/propagated/extra", "-I=$(S)/propagated/extra", *END_INCLUDES,
        ])

    def test_repeated_local_paths_are_unique_but_peer_overlap_is_retained(self):
        for with_gen in (False, True):
            with self.subTest(with_gen=with_gen):
                option = "WITH_GEN " if with_gen else ""
                files = world(f"PROTO_NAMESPACE({option}{ROOT})\nPROTO_ADDINCL({option}{ROOT})\nPEERDIR(support/same)\n")
                files["support/same/ya.make"] = (
                    f"PROTO_LIBRARY()\n{lib.BARE_MODULE}ONLY_TAGS(PY_PROTO PY3_PROTO)\n"
                    f"PROTO_NAMESPACE({option}{ROOT})\nEND()\n"
                )
                graph = lib.make(files, "client3")
                local = ([f"-I=$(B)/{ROOT}"] if with_gen else []) + [f"-I=$(S)/{ROOT}"]
                self.assertEqual(includes(producer(graph)), prefix() + local + local + END_INCLUDES)

    def test_output_namespace_alone_does_not_create_an_include_directory(self):
        for body, root, local in (
            (f"PROTO_NAMESPACE({ROOT})\n", ROOT, [f"-I=$(S)/{ROOT}"]),
            (f"SET(PROTO_NAMESPACE {ROOT})\n", ROOT, []),
            (f"PROTO_NAMESPACE(.)\nSET(PROTO_NAMESPACE {ROOT})\n", ROOT, []),
            ("PROTO_NAMESPACE(WITH_GEN .)\n", "", ["-I=$(B)"]),
            (f"PROTO_ADDINCL(WITH_GEN .)\nSET(PROTO_NAMESPACE {ROOT})\n", ROOT, ["-I=$(B)"]),
        ):
            with self.subTest(body=body):
                files = world(body)
                files[f"{ROOT}/schema/packet.proto"] = 'syntax = "proto2";\nmessage Packet {}\n'
                graph = lib.make(files, "client3")
                self.assertEqual(includes(producer(graph)), prefix(root) + local + END_INCLUDES)

    def test_untyped_peer_header_include_does_not_duplicate_proto_namespace(self):
        files = world(f"PROTO_NAMESPACE({ROOT})\nPEERDIR(support/native)\n")
        files["support/native/ya.make"] = (
            f"LIBRARY()\n{lib.BARE_MODULE}ADDINCL(GLOBAL ${{ARCADIA_BUILD_ROOT}}/{ROOT})\nEND()\n"
        )
        graph = lib.make(files, "client3")
        self.assertEqual(includes(producer(graph)), prefix() + [f"-I=$(S)/{ROOT}"] + END_INCLUDES)

    def test_resolve_proto_adds_its_declared_local_include_in_place(self):
        files = world("ADDINCL(FOR proto manual/first)\nRESOLVE_PROTO()\n"
                      f"PROTO_NAMESPACE(WITH_GEN {ROOT})\n")
        graph = lib.make(files, "client3")
        self.assertEqual(includes(producer(graph)), [
            *prefix(), "-I=$(S)/manual/first", "-I=$(S)/contrib/libs/protobuf/src",
            f"-I=$(B)/{ROOT}", f"-I=$(S)/{ROOT}", *END_INCLUDES,
        ])

    def test_shared_lexical_include_keeps_each_modules_local_plan(self):
        files = world()
        files["shared/proto.inc"] = "ADDINCL(FOR proto ${FIRST_INCLUDE})\nPROTO_NAMESPACE(WITH_GEN ${WIRE_ROOT})\n"
        for name in ("red", "blue"):
            module, root = f"contracts/{name}", f"wire/{name}"
            files[f"{module}/ya.make"] = (
                f"PROTO_LIBRARY()\n{lib.BARE_MODULE}ONLY_TAGS(PY3_PROTO)\nNO_MYPY()\n"
                f"SET(FIRST_INCLUDE manual/{name})\nSET(WIRE_ROOT {root})\n"
                "INCLUDE(${ARCADIA_ROOT}/shared/proto.inc)\n"
                f"SRCDIR({root})\nSRCS(packet.proto)\nEND()\n"
            )
            files[f"{root}/packet.proto"] = 'syntax = "proto2";\nmessage Packet {}\n'
        files["client3/ya.make"] = (
            f"PY3_PROGRAM()\n{lib.BARE_MODULE}NO_CHECK_IMPORTS()\nPEERDIR(contracts/red contracts/blue)\nEND()\n"
        )
        graph = lib.make(files, "client3")
        for name in ("red", "blue"):
            root = f"wire/{name}"
            self.assertEqual(includes(producer(graph, source="packet", root=root)), [
                *prefix(root), f"-I=$(S)/manual/{name}", f"-I=$(B)/{root}", f"-I=$(S)/{root}", *END_INCLUDES,
            ])

    def test_generated_proto_consumers_keep_registered_producer_edges(self):
        files = world()
        files[f"{MODULE}/ya.make"] = (
            f"PROTO_LIBRARY()\n{lib.BARE_MODULE}ONLY_TAGS(PY3_PROTO)\nNO_MYPY()\n"
            f"PROTO_NAMESPACE(WITH_GEN {MODULE})\nPY_PROTO_PLUGIN(letter _stub.py {TOOL})\n"
            "RUN_PYTHON3(emit.py OUT_NOAUTO generated.proto)\nSRCS(generated.proto)\nEND()\n"
        )
        files[f"{MODULE}/emit.py"] = ""
        graph = lib.make(files, "client3")
        generated = lib.node_by_output(graph, f"$(B)/{MODULE}/generated.proto")
        node = producer(graph, source="generated", root=MODULE)
        self.assertEqual(includes(node), prefix(MODULE) + [f"-I=$(B)/{MODULE}", f"-I=$(S)/{MODULE}"] + END_INCLUDES)
        self.assertEqual(node["cmds"][0]["cwd"], "$(B)")
        self.assertIn(generated["uid"], node["deps"])
        self.assertIn(f"$(B)/{MODULE}/generated.proto", node["inputs"])
        self.assertNotIn(f"$(S)/{MODULE}/generated.proto", node["inputs"])
        self.assertIn(f"$(S)/{MODULE}/emit.py", generated["inputs"])
        stub = f"$(B)/{MODULE}/generated__intpy3___stub.py"
        consumers = [n for n in graph["graph"] if stub in n["inputs"]]
        self.assertTrue(consumers)
        for consumer in consumers:
            self.assertIn(node["uid"], consumer["deps"])
            self.assertIn(node["outputs"][0], consumer["inputs"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
