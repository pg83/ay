import unittest

import lib


STUB_LIBRARY = "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n"

PEER_STUBS = (
    "contrib/libs/googleapis-common-protos",
    "contrib/libs/grpc",
    "contrib/libs/protobuf",
    "contrib/libs/python",
    "contrib/python/grpcio",
    "contrib/python/protobuf",
    "contrib/tools/python3/Modules/_sqlite",
    "library/cpp/malloc/jemalloc",
    "library/cpp/resource",
    "library/python/import_tracing/constructor",
    "library/python/runtime_py3/main",
    "library/python/testing/import_test",
)

TOOLS = (
    "contrib/python/mypy-protobuf/bin/protoc-gen-mypy",
    "contrib/tools/protoc",
    "contrib/tools/protoc/plugins/grpc_python",
    "tools/archiver",
    "tools/py3cc",
    "tools/py3cc/slow",
    "tools/rescompiler",
    "tools/rescompressor",
)

PY3CC = "$(B)/tools/py3cc/py3cc --slow-py3cc $(B)/tools/py3cc/slow/slow"
PROTOC = "$(B)/contrib/tools/protoc/protoc"
MYPY = "$(B)/contrib/python/mypy-protobuf/bin/protoc-gen-mypy/protoc-gen-mypy"
GRPC_PY = "$(B)/contrib/tools/protoc/plugins/grpc_python/grpc_python"
WRAPPER = "$(S)/build/scripts/gen_py_protos.py"


def python_base():
    files = {f"{path}/ya.make": STUB_LIBRARY for path in PEER_STUBS}
    for path in TOOLS:
        lib.tool_program(files, path, path.rsplit("/", 1)[-1])
    files["app/ya.make"] = (
        "PY3_PROGRAM()\n"
        "PY_SRCS(MAIN main.py)\n"
        "PEERDIR(proto)\n"
        "END()\n"
    )
    files["app/main.py"] = ""
    files["build/scripts/gen_py_protos.py"] = ""
    return files


def args(node):
    return node["cmds"][0]["cmd_args"]


def rescompiler_kvs(node):
    return [arg for arg in args(node) if arg.startswith("resfs/src/")]


class PyProtoTest(unittest.TestCase):
    def test_proto_library_with_grpc_and_namespaces(self):
        files = python_base()
        files.update({
            "proto/ya.make": (
                "PROTO_LIBRARY()\n"
                "GRPC()\n"
                "PROTO_NAMESPACE(proto)\n"
                "PY_NAMESPACE(my.ns)\n"
                "SRCS(x.proto sub/y.proto)\n"
                "EXCLUDE_TAGS(CPP_PROTO)\n"
                "END()\n"
            ),
            "proto/x.proto": 'syntax = "proto3";\nimport "sub/y.proto";\nmessage X {}\n',
            "proto/sub/y.proto": 'syntax = "proto3";\nmessage Y {}\n',
        })
        graph = lib.make(files, "app")

        x_out = "$(B)/proto/x__intpy3___pb2.py"
        pb = lib.node_by_output(graph, x_out)
        self.assertEqual(pb["kv"]["p"], "PB")
        self.assertEqual(pb["outputs"], [
            x_out,
            "$(B)/proto/x__intpy3___pb2_grpc.py",
            "$(B)/proto/x__intpy3___pb2.pyi",
        ])
        self.assertEqual(pb["cmds"][0]["cwd"], "$(S)")
        self.assertEqual(args(pb)[1:], [
            WRAPPER, "--py_ver", "py3",
            "--suffixes", "_pb2.py", "_pb2_grpc.py", "_pb2.pyi",
            "--input", "proto/x.proto",
            "--ns", "/proto", "--", PROTOC,
            "-I=./proto", "-I=$(S)/proto", "-I=$(B)", "-I=$(S)", "-I=$(S)/proto",
            "-I=$(S)/contrib/libs/protoc/src", "-I=$(B)", "-I=$(S)/contrib/libs/protobuf/src",
            "--python_out=$(B)/proto",
            "proto/x.proto",
            f"--plugin=protoc-gen-grpc_py={GRPC_PY}",
            "--grpc_py_out=$(B)/proto",
            f"--plugin=protoc-gen-mypy={MYPY}",
            "--mypy_out=$(B)/proto",
        ])
        for expected in (PROTOC, GRPC_PY, MYPY, WRAPPER, "$(S)/proto/x.proto", "$(S)/proto/sub/y.proto"):
            self.assertIn(expected, pb["inputs"])
        self.assertEqual(pb["kv"]["ext_out_name_for_x__intpy3___pb2.py"], "x_pb2.py")
        self.assertEqual(pb["kv"]["ext_out_name_for_x__intpy3___pb2_grpc.py"], "x_pb2_grpc.py")
        self.assertEqual(pb["kv"]["ext_out_name_for_x__intpy3___pb2.pyi"], "x_pb2.pyi")
        self.assertEqual(sorted(pb["foreign_deps"]["tool"]), sorted(pb["deps"]))

        grpc_pyc = lib.node_by_output(graph, "$(B)/proto/x__intpy3___pb2_grpc.py.sd3w.yapyc3")
        self.assertEqual(
            " ".join(args(grpc_pyc)),
            f"{PY3CC} proto/x__intpy3___pb2_grpc.py- $(B)/proto/x__intpy3___pb2_grpc.py "
            "$(B)/proto/x__intpy3___pb2_grpc.py.sd3w.yapyc3",
        )
        self.assertIn(x_out, grpc_pyc["inputs"])
        self.assertIn("$(S)/proto/sub/y.proto", grpc_pyc["inputs"])

        packs = [
            node for node in graph["graph"]
            if node["kv"].get("p") == "PR" and node["outputs"][0].startswith("$(B)/proto/")
        ]
        self.assertEqual(len(packs), 1)
        self.assertEqual(rescompiler_kvs(packs[0]), [
            "resfs/src/resfs/file/py/my/ns/x_pb2.py=proto/x__intpy3___pb2.py",
            "resfs/src/resfs/file/py/my/ns/x_pb2.py.yapyc3=proto/x__intpy3___pb2.py.sd3w.yapyc3",
            "resfs/src/resfs/file/py/my/ns/x_pb2_grpc.py=proto/x__intpy3___pb2_grpc.py",
            "resfs/src/resfs/file/py/my/ns/x_pb2_grpc.py.yapyc3"
            "=proto/x__intpy3___pb2_grpc.py.sd3w.yapyc3",
            "resfs/src/resfs/file/py/my/ns/sub/y_pb2.py=proto/sub/y__intpy3___pb2.py",
            "resfs/src/resfs/file/py/my/ns/sub/y_pb2.py.yapyc3"
            "=proto/sub/y__intpy3___pb2.py.sd3w.yapyc3",
            "resfs/src/resfs/file/py/my/ns/sub/y_pb2_grpc.py=proto/sub/y__intpy3___pb2_grpc.py",
            "resfs/src/resfs/file/py/my/ns/sub/y_pb2_grpc.py.yapyc3"
            "=proto/sub/y__intpy3___pb2_grpc.py.sd3w.yapyc3",
        ])
        aux = lib.node_by_output(graph, packs[0]["outputs"][0] + ".py3.o")
        global_archive = lib.node_by_output(graph, "$(B)/proto/libpy3proto.global.a")
        self.assertEqual(global_archive["inputs"][0], aux["outputs"][0])

    def test_proto_in_py_srcs_packs_group_resources(self):
        files = python_base()
        files.update({
            "lib/ya.make": (
                "PY3_LIBRARY()\n"
                "NO_MYPY()\n"
                "PY_SRCS(NAMESPACE q a.py x.proto)\n"
                "END()\n"
            ),
            "lib/a.py": "",
            "lib/x.proto": 'syntax = "proto3";\nmessage X {}\n',
        })
        graph = lib.make(files, "lib")
        pb = lib.node_by_output(graph, "$(B)/lib/x__intpy3___pb2.py")
        self.assertEqual(pb["outputs"], ["$(B)/lib/x__intpy3___pb2.py"])
        self.assertEqual(args(pb)[1:6], [WRAPPER, "--py_ver", "py3", "--suffixes", "_pb2.py"])
        self.assertNotIn("--mypy_out=$(B)/", args(pb))
        self.assertEqual(pb["inputs"][0], PROTOC)
        self.assertNotIn(MYPY, pb["inputs"])

        pyc = lib.node_by_output(graph, "$(B)/lib/x__intpy3___pb2.py.zsw2.yapyc3")
        self.assertEqual(args(pyc)[-3:], [
            "lib/x__intpy3___pb2.py-",
            "$(B)/lib/x__intpy3___pb2.py",
            "$(B)/lib/x__intpy3___pb2.py.zsw2.yapyc3",
        ])
        packs = [
            node for node in graph["graph"]
            if node["kv"].get("p") == "PR" and node["outputs"][0].startswith("$(B)/lib/")
        ]
        self.assertEqual(len(packs), 1)
        self.assertEqual(rescompiler_kvs(packs[0]), [
            "resfs/src/resfs/file/py/q/x_pb2.py=lib/x__intpy3___pb2.py",
            "resfs/src/resfs/file/py/q/x_pb2.py.yapyc3=lib/x__intpy3___pb2.py.zsw2.yapyc3",
        ])
        aux = lib.node_by_output(graph, packs[0]["outputs"][0] + ".o")
        self.assertIn("$(S)/lib/x.proto", aux["inputs"])
        archive = lib.node_by_output(graph, "$(B)/lib/libpy3lib.global.a")
        self.assertIn(aux["outputs"][0], archive["inputs"])

    def test_py23_library_proto_aux_uses_py3_suffix(self):
        files = python_base()
        files.update({
            "lib/ya.make": (
                "PY23_LIBRARY()\n"
                "PY_SRCS(a.py x.proto)\n"
                "END()\n"
            ),
            "lib/a.py": "",
            "lib/x.proto": 'syntax = "proto3";\nmessage X {}\n',
        })
        files["app/ya.make"] = files["app/ya.make"].replace("PEERDIR(proto)", "PEERDIR(lib)")
        graph = lib.make(files, "app")
        packs = [
            node for node in graph["graph"]
            if node["kv"].get("p") == "PR" and node["outputs"][0].startswith("$(B)/lib/")
        ]
        self.assertEqual(len(packs), 1)
        self.assertEqual(rescompiler_kvs(packs[0]), [
            "resfs/src/resfs/file/py/lib/x_pb2.py=lib/x__intpy3___pb2.py",
            "resfs/src/resfs/file/py/lib/x_pb2.py.yapyc3=lib/x__intpy3___pb2.py.zsw2.yapyc3",
        ])
        aux = lib.node_by_output(graph, packs[0]["outputs"][0] + ".py3.o")
        self.assertEqual(aux["kv"]["p"], "CC")

    def test_generated_proto_runs_protoc_in_build_root(self):
        files = python_base()
        files.update({
            "proto/ya.make": (
                "PROTO_LIBRARY()\n"
                "PY_NAMESPACE(.)\n"
                "RUN_PYTHON3(gen.py OUT gen.proto)\n"
                "EXCLUDE_TAGS(CPP_PROTO)\n"
                "END()\n"
            ),
            "proto/gen.py": "",
        })
        graph = lib.make(files, "app")
        producer = lib.node_by_output(graph, "$(B)/proto/gen.proto")
        pb = lib.node_by_output(graph, "$(B)/proto/gen__intpy3___pb2.py")
        self.assertEqual(pb["cmds"][0]["cwd"], "$(B)")
        self.assertIn("$(B)/proto/gen.proto", pb["inputs"])
        self.assertIn("$(S)/proto/gen.py", pb["inputs"])
        self.assertNotIn("$(S)/proto/gen.proto", pb["inputs"])
        self.assertIn(producer["uid"], pb["deps"])

        pyc = lib.node_by_output(graph, "$(B)/proto/gen__intpy3___pb2.py.yapyc3")
        self.assertEqual(args(pyc)[-3:], [
            "gen__intpy3___pb2.py-",
            "$(B)/proto/gen__intpy3___pb2.py",
            "$(B)/proto/gen__intpy3___pb2.py.yapyc3",
        ])
        objcopy = lib.node_by_output_prefix(graph, "$(B)/proto/objcopy_")
        kvs = args(objcopy)[args(objcopy).index("--kvs") + 1:]
        self.assertEqual(kvs, [
            "resfs/src/resfs/file/py/gen_pb2.py=proto/gen__intpy3___pb2.py",
            "resfs/src/resfs/file/py/gen_pb2.py.yapyc3=proto/gen__intpy3___pb2.py.yapyc3",
        ])
        self.assertIn("$(B)/proto/gen__intpy3___pb2.py", objcopy["inputs"])

    def test_include_flags_from_peers_and_macros(self):
        files = python_base()
        files.update({
            "proto/ya.make": (
                "PROTO_LIBRARY()\n"
                "PROTO_NAMESPACE(proto)\n"
                "NO_MYPY()\n"
                "GRPC()\n"
                "USE_COMMON_GOOGLE_APIS()\n"
                "SET_APPEND(_PROTOC_FLAGS --experimental_allow_proto3_optional)\n"
                "PEERDIR(proto/dep)\n"
                "SRCS(x.proto x.proto)\n"
                "EXCLUDE_TAGS(CPP_PROTO)\n"
                "END()\n"
            ),
            "proto/x.proto": 'syntax = "proto3";\nmessage X {}\n',
            "proto/dep/ya.make": (
                "PROTO_LIBRARY()\n"
                "PROTO_NAMESPACE(proto)\n"
                "ADDINCL(GLOBAL FOR proto extra GLOBAL FOR proto contrib/libs/protoc/src)\n"
                "SRCS(d.proto)\n"
                "EXCLUDE_TAGS(CPP_PROTO)\n"
                "END()\n"
            ),
            "proto/dep/d.proto": 'syntax = "proto3";\n',
        })
        graph = lib.make(files, "app")
        pb = lib.node_by_output(graph, "$(B)/proto/x__intpy3___pb2.py")
        self.assertEqual(pb["outputs"], [
            "$(B)/proto/x__intpy3___pb2.py", "$(B)/proto/x__intpy3___pb2_grpc.py",
        ])
        self.assertEqual(args(pb)[args(pb).index("--suffixes"):args(pb).index("--input")], [
            "--suffixes", "_pb2.py", "_pb2_grpc.py",
        ])
        self.assertEqual(args(pb)[args(pb).index("--"):], [
            "--", PROTOC,
            "-I=./proto", "-I=$(S)/proto", "-I=$(B)", "-I=$(S)",
            "-I=$(S)/contrib/libs/googleapis-common-protos",
            "-I=$(S)/proto", "-I=$(S)/proto",
            "-I=$(S)/extra", "-I=$(S)/contrib/libs/protoc/src",
            "-I=$(B)", "-I=$(S)/contrib/libs/protobuf/src",
            "--python_out=$(B)/proto",
            "--experimental_allow_proto3_optional",
            "proto/x.proto",
            f"--plugin=protoc-gen-grpc_py={GRPC_PY}",
            "--grpc_py_out=$(B)/proto",
        ])
        pack = [
            node for node in graph["graph"]
            if node["kv"].get("p") == "PR" and node["outputs"][0].startswith("$(B)/proto/")
            and "$(B)/proto/x__intpy3___pb2.py" in node["inputs"]
        ]
        self.assertEqual(len(pack), 1)
        self.assertEqual(rescompiler_kvs(pack[0]).count(
            "resfs/src/resfs/file/py/proto/x_pb2.py=proto/x__intpy3___pb2.py"), 2)
        self.assertEqual(rescompiler_kvs(pack[0]).count(
            "resfs/src/resfs/file/py/proto/x_pb2_grpc.py=proto/x__intpy3___pb2_grpc.py"), 2)

    def test_disabled_google_proto_peerdirs_drop_protoc_include(self):
        files = python_base()
        files.update({
            "proto/ya.make": (
                "PROTO_LIBRARY()\n"
                "DISABLE(NEED_GOOGLE_PROTO_PEERDIRS)\n"
                "SRCS(x.proto)\n"
                "EXCLUDE_TAGS(CPP_PROTO)\n"
                "END()\n"
            ),
            "proto/x.proto": 'syntax = "proto3";\nmessage X {}\n',
        })
        graph = lib.make(files, "app")
        pb = lib.node_by_output(graph, "$(B)/proto/x__intpy3___pb2.py")
        self.assertNotIn("-I=$(S)/contrib/libs/protoc/src", args(pb))
        self.assertIn("-I=$(S)/contrib/libs/protobuf/src", args(pb))


if __name__ == "__main__":
    unittest.main(verbosity=2)
