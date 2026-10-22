import unittest

import lib
from test_proto_desc import desc_files
from test_proto_py2_context import proto_world
from test_python_proto import MYPY, PROTOC, WRAPPER, args


MODULE = "contracts/parcel-api"
TREE = "wire-tree"
TOOL = "generators/rpc-backend"
TOOL_BINARY = "$(B)/generators/rpc-backend/parcel-plugin"


def plugin_world():
    files = proto_world()
    files.update(desc_files())
    lib.tool_program(files, TOOL, "parcel-plugin")
    files[f"{MODULE}/ya.make"] = (
        f"PROTO_LIBRARY()\n{lib.BARE_MODULE}NO_OPTIMIZE_PY_PROTOS()\n"
        "ONLY_TAGS(PY_PROTO PY3_PROTO DESC_PROTO)\n"
        f"PROTO_NAMESPACE(WITH_GEN {TREE})\nPY_NAMESPACE(public.rpc)\nSRCDIR({TREE})\n"
        "SET(RPC_NAME parcel)\nSET(RPC_EXT _stub.py)\n"
        f"SET(RPC_TOOL {TOOL})\nSET(RPC_DEPS runtime/rpc)\n"
        "INCLUDE(plugin.inc)\nSRCS(nested/packet.proto second.proto)\nEND()\n"
    )
    files[f"{MODULE}/plugin.inc"] = "PY_PROTO_PLUGIN($RPC_NAME $RPC_EXT $RPC_TOOL DEPS $RPC_DEPS)\n"
    files[f"{TREE}/nested/packet.proto"] = 'syntax = "proto2";\nimport "second.proto";\nmessage Parcel {}\n'
    files[f"{TREE}/second.proto"] = 'syntax = "proto2";\nmessage Second {}\n'
    files["runtime/rpc/ya.make"] = (
        f"PY23_LIBRARY()\n{lib.BARE_MODULE}NO_PYTHON_INCLUDES()\n"
        "RESOURCE(DONT_COMPRESS - plugin_runtime=parcel)\nEND()\n"
    )
    for target, version in (("client2", "PY2"), ("client3", "PY3")):
        files[f"{target}/ya.make"] = (
            f"{version}_PROGRAM()\n{lib.BARE_MODULE}NO_CHECK_IMPORTS()\nPEERDIR({MODULE})\nEND()\n"
        )
    return files


class PythonProtoPluginTest(unittest.TestCase):
    def test_single_plugin_shared_lexical_call_in_both_python_contexts(self):
        files = plugin_world()
        files[f"{MODULE}/ya.make"] = files[f"{MODULE}/ya.make"].replace(
            "SET(RPC_NAME parcel)",
            "IF(PYTHON2)\nSET(RPC_NAME parcel2)\nELSE()\nSET(RPC_NAME parcel3)\nENDIF()",
        )
        graph = lib.make(files, "client2", "client3")
        tool = lib.node_by_output(graph, TOOL_BINARY)
        for version, plugin, protoc in (("py2", "parcel2", "$(B)/contrib/tools/protoc_old/protoc_old"),
                                        ("py3", "parcel3", PROTOC)):
            with self.subTest(version=version):
                stem = f"$(B)/{TREE}/nested/packet__int{version}__"
                main, stub, mypy = [stem + suffix for suffix in ("_pb2.py", "_stub.py", "_pb2.pyi")]
                producer = lib.node_by_output(graph, stub)
                self.assertEqual(producer["outputs"], [main, stub, mypy])
                command = args(producer)
                self.assertEqual(command[1:], [
                    WRAPPER, "--py_ver", version, "--suffixes", "_pb2.py", "_stub.py", "_pb2.pyi",
                    "--input", f"{TREE}/nested/packet.proto", "--ns", f"/{TREE}", "--", protoc,
                    f"-I=./{TREE}", f"-I=$(S)/{TREE}", "-I=$(B)", "-I=$(S)",
                    f"-I=$(S)/{TREE}", f"-I=$(B)/{TREE}",
                    "-I=$(B)", "-I=$(S)/contrib/libs/protobuf/src", f"--python_out=$(B)/{TREE}",
                    f"{TREE}/nested/packet.proto", f"--plugin=protoc-gen-{plugin}={TOOL_BINARY}",
                    f"--{plugin}_out=$(B)/{TREE}",
                    f"--plugin=protoc-gen-mypy={MYPY}", f"--mypy_out=$(B)/{TREE}",
                ])
                self.assertEqual(producer["cmds"][0]["cwd"], "$(S)")
                for source in (f"$(S)/{TREE}/nested/packet.proto", f"$(S)/{TREE}/second.proto", WRAPPER,
                               protoc, TOOL_BINARY, MYPY):
                    self.assertIn(source, producer["inputs"])
                self.assertIn(tool["uid"], producer["foreign_deps"]["tool"])
                self.assertEqual(sorted(producer["deps"]), sorted(producer["foreign_deps"]["tool"]))
                self.assertEqual(producer["kv"][f"ext_out_name_for_packet__int{version}___stub.py"], "packet_stub.py")
                bytecode = next(n for n in graph["graph"] if stub in args(n)
                                and any(o.endswith(".yapyc" if version == "py2" else ".yapyc3") for o in n["outputs"]))
                self.assertIn(main, bytecode["inputs"])
                self.assertIn(producer["uid"], bytecode["deps"])
                self.assertNotIn(f"$(S)/{TREE}/nested/packet.proto", bytecode["inputs"])
                resource_key = ("resfs/src//py_modules/public.rpc.nested.packet_stub=" if version == "py2"
                                else "resfs/src/resfs/file/py/public/rpc/nested/packet_stub.py=")
                self.assertTrue(any(resource_key + stub.removeprefix("$(B)/") in args(n) for n in graph["graph"]))
                lib.node_by_output(graph, f"$(B)/{TREE}/second__int{version}___stub.py")
                link = lib.node_by_output(graph, f"$(B)/client{2 if version == 'py2' else 3}/client{2 if version == 'py2' else 3}")
                self.assertTrue(any(o.startswith("$(B)/runtime/rpc/") for o in link["inputs"]))
                self.assertTrue(any(o.startswith("$(B)/contrib/python/protobuf/") for o in link["inputs"]))

    def test_plugin2_generated_proto_and_secondary_consumers(self):
        files = plugin_world()
        files[f"{MODULE}/ya.make"] = (
            f"PROTO_LIBRARY()\n{lib.BARE_MODULE}NO_OPTIMIZE_PY_PROTOS()\nNO_MYPY()\n"
            "ONLY_TAGS(PY_PROTO PY3_PROTO)\nPY_NAMESPACE(public.generated)\n"
            f"PROTO_NAMESPACE(WITH_GEN {MODULE})\n"
            f"PY_PROTO_PLUGIN2(parcel _stub.py _client.py {TOOL} DEPS runtime/rpc)\n"
            "RUN_PYTHON3(emit.py OUT_NOAUTO generated.proto OUTPUT_INCLUDES imported.proto)\n"
            "SRCS(generated.proto)\n"
            "RESOURCE(DONT_COMPRESS generated__intpy3___client.py plugin_client)\nEND()\n"
        )
        files[f"{MODULE}/emit.py"] = ""
        files[f"{MODULE}/imported.proto"] = 'syntax = "proto2";\nmessage Imported {}\n'
        graph = lib.make(files, "client3")
        proto = lib.node_by_output(graph, f"$(B)/{MODULE}/generated.proto")
        self.assertIn(f"$(S)/{MODULE}/emit.py", proto["inputs"])
        stem = f"$(B)/{MODULE}/generated__intpy3__"
        main, stub, client = [stem + suffix for suffix in ("_pb2.py", "_stub.py", "_client.py")]
        producer = lib.node_by_output(graph, client)
        self.assertEqual(producer["outputs"], [main, stub, client])
        self.assertEqual(args(producer)[5:8], ["_pb2.py", "_stub.py", "_client.py"])
        self.assertEqual(producer["cmds"][0]["cwd"], "$(B)")
        self.assertIn(proto["uid"], producer["deps"])
        self.assertIn(f"$(B)/{MODULE}/generated.proto", producer["inputs"])
        self.assertIn(f"$(S)/{MODULE}/imported.proto", producer["inputs"])
        self.assertNotIn(f"$(S)/{MODULE}/emit.py", producer["inputs"])
        self.assertNotIn(f"$(S)/{MODULE}/generated.proto", producer["inputs"])
        consumers = [n for n in graph["graph"] if client in n["inputs"]]
        self.assertGreaterEqual(len(consumers), 2)
        for consumer in consumers:
            self.assertEqual(consumer["inputs"].count(main), 1)
            self.assertEqual(consumer["deps"].count(producer["uid"]), 1)
        self.assertEqual(producer["kv"]["ext_out_name_for_generated__intpy3___client.py"], "generated_client.py")

    def test_plugins_and_grpc_keep_declaration_order(self):
        files = plugin_world()
        files[f"{MODULE}/ya.make"] = (
            f"PROTO_LIBRARY()\n{lib.BARE_MODULE}NO_OPTIMIZE_PY_PROTOS()\nNO_MYPY()\n"
            f"PY_PROTO_PLUGIN(before _before.py {TOOL})\nGRPC()\n"
            f"PY_PROTO_PLUGIN2(after _after.py _last.py {TOOL})\n"
            f"SRCDIR({TREE})\nSRCS(second.proto)\nEND()\n"
        )
        graph = lib.make(files, "client3")
        suffixes = ["_pb2.py", "_before.py", "_pb2_grpc.py", "_after.py", "_last.py"]
        producer = lib.node_by_output(graph, f"$(B)/{TREE}/second__intpy3___last.py")
        self.assertEqual(producer["outputs"], [f"$(B)/{TREE}/second__intpy3__{s}" for s in suffixes])
        command = args(producer)
        self.assertEqual(command[command.index("--suffixes") + 1:command.index("--input")], suffixes)
        self.assertEqual(command[-6:], [
            f"--plugin=protoc-gen-before={TOOL_BINARY}", "--before_out=$(B)/",
            "--plugin=protoc-gen-grpc_py=$(B)/contrib/tools/protoc/plugins/grpc_python/grpc_python",
            "--grpc_py_out=$(B)/", f"--plugin=protoc-gen-after={TOOL_BINARY}", "--after_out=$(B)/",
        ])
        self.assertNotIn(MYPY, producer["inputs"])
        self.assertEqual(producer["inputs"].count(TOOL_BINARY), 2)

    def test_shared_sources_reuse_registered_plugin_producer(self):
        files = plugin_world()
        other = "contracts/another-api"
        for directory, namespace in ((MODULE, "public.first"), (other, "public.second")):
            files[f"{directory}/ya.make"] = (
                f"PROTO_LIBRARY()\n{lib.BARE_MODULE}NO_OPTIMIZE_PY_PROTOS()\nNO_MYPY()\n"
                f"PY_NAMESPACE({namespace})\nSRCDIR({TREE})\nINCLUDE(${{ARCADIA_ROOT}}/lexical/plugin.inc)\n"
                "SRCS(second.proto)\nEND()\n"
            )
        files["lexical/plugin.inc"] = f"PY_PROTO_PLUGIN(parcel _stub.py {TOOL})\n"
        files["client3/ya.make"] = files["client3/ya.make"].replace(f"PEERDIR({MODULE})", f"PEERDIR({MODULE} {other})")
        graph = lib.make(files, "client3")
        main, stub = [f"$(B)/{TREE}/second__intpy3__{s}" for s in ("_pb2.py", "_stub.py")]
        producer = lib.node_by_output(graph, stub)
        self.assertEqual(sum(stub in n["outputs"] for n in graph["graph"]), 1)
        for namespace in ("first", "second"):
            resource = next(n for n in graph["graph"] if
                            f"resfs/src/resfs/file/py/public/{namespace}/second_stub.py={stub.removeprefix('$(B)/')}" in args(n))
            self.assertIn(main, resource["inputs"])
            self.assertIn(producer["uid"], resource["deps"])

    def test_plain_python_sources_and_deps_are_deferred_until_a_proto(self):
        for with_proto in (False, True):
            with self.subTest(with_proto=with_proto):
                files = plugin_world()
                declaration = (f"PY_PROTO_PLUGIN(parcel _stub.py {TOOL} DEPS runtime/rpc)\n" if with_proto else
                               "PY_PROTO_PLUGIN(absent _stub.py missing/plugin DEPS missing/runtime)\n")
                files["client3/ya.make"] = (
                    f"PY3_PROGRAM()\n{lib.BARE_MODULE}NO_CHECK_IMPORTS()\nNO_MYPY()\n"
                    + declaration + f"PY_SRCS(main.py{' packet.proto' if with_proto else ''})\nEND()\n"
                )
                files["client3/main.py"] = ""
                files["client3/packet.proto"] = 'syntax = "proto2";\n'
                graph = lib.make(files, "client3")
                if with_proto:
                    producer = lib.node_by_output(graph, "$(B)/client3/packet__intpy3___stub.py")
                    self.assertIn(TOOL_BINARY, producer["inputs"])
                    link = lib.node_by_output(graph, "$(B)/client3/client3")
                    self.assertTrue(any(i.startswith("$(B)/runtime/rpc/") for i in link["inputs"]))
                else:
                    self.assertFalse(any("missing/" in i for n in graph["graph"] for i in n["inputs"]))

    def test_malformed_calls_report_the_macro_arity(self):
        for macro, arguments, required in (("PY_PROTO_PLUGIN", "parcel _stub.py", 3),
                                           ("PY_PROTO_PLUGIN2", "parcel _stub.py tool", 4),
                                           ("PY_PROTO_PLUGIN", "parcel _stub.py tool surplus", 3)):
            with self.subTest(macro=macro, arguments=arguments):
                files = plugin_world()
                files[f"{MODULE}/plugin.inc"] = f"{macro}({arguments})\n"
                result = lib.make_process(files, "client3")
                self.assertEqual(result.returncode, 1)
                self.assertIn(f"{macro} expects {required} positional arguments", result.stderr)

    def test_ignored_cpp_descriptor_and_plain_contexts(self):
        for target, declaration in (("native", f"LIBRARY()\n{lib.BARE_MODULE}"),
                                    ("descriptions", "PROTO_DESCRIPTIONS()\n"),
                                    ("plain", f"LIBRARY()\n{lib.BARE_MODULE}")):
            with self.subTest(target=target):
                files = plugin_world()
                files[f"{target}/ya.make"] = declaration + f"PEERDIR({MODULE})\nEND()\n"
                files[f"{MODULE}/ya.make"] = (
                    f"PROTO_LIBRARY()\n{lib.BARE_MODULE}NO_MYPY()\nSRCDIR({TREE})\nSRCS(second.proto)\n"
                    "INCLUDE(plugin.inc)\nEND()\n"
                )
                # None of these paths exist: an ignored context must not request them.
                files[f"{MODULE}/plugin.inc"] = (
                    "PY_PROTO_PLUGIN(absent _phantom.py missing/plugin DEPS missing/runtime)\n"
                    "PY_PROTO_PLUGIN2(absent2 _left.py _right.py missing/plugin2 DEPS missing/runtime2)\n"
                )
                if target == "plain":
                    files[f"{target}/ya.make"] = declaration + "INCLUDE(other.inc)\nEND()\n"
                    files[f"{target}/other.inc"] = files[f"{MODULE}/plugin.inc"]
                graph = lib.make(files, target)
                self.assertFalse(any("phantom" in o or "missing/" in o or o.endswith(("_left.py", "_right.py"))
                                     for n in graph["graph"] for o in n["outputs"]))
                self.assertFalse(any("protoc-gen-absent" in a for n in graph["graph"] for a in args(n)))


if __name__ == "__main__":
    unittest.main(verbosity=2)
