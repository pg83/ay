import json
import os
import tempfile
import unittest
from pathlib import Path

import lib


NO_PLATFORM = "NO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
TOOLS = [
    "contrib/python/mypy-protobuf/bin/protoc-gen-mypy",
    "contrib/tools/protoc",
    "contrib/tools/protoc/plugins/cpp_styleguide",
    "contrib/tools/protoc/plugins/grpc_cpp",
    "contrib/tools/protoc/plugins/grpc_python",
    "tools/archiver",
    "tools/py3cc",
    "tools/py3cc/slow",
    "tools/rescompiler",
    "tools/rescompressor",
]
LIBRARIES = [
    "contrib/libs/protobuf",
    "contrib/libs/python",
    "library/cpp/resource",
    "contrib/tools/python3/Modules/_sqlite",
    "library/cpp/malloc/jemalloc",
    "library/python/import_tracing/constructor",
    "library/python/runtime_py3",
    "library/python/runtime_py3/main",
    "library/python/testing/import_test",
]
PLAIN_FILES = [
    "build/scripts/cpp_proto_wrapper.py",
    "build/scripts/gen_py_protos.py",
] + [
    f"contrib/libs/protobuf/src/google/protobuf/{name}"
    for name in (
        "generated_message_bases.h", "map_entry.h", "map_entry_lite.h",
        "map_field.h", "map_field_inl.h", "map_field_lite.h", "reflection_ops.h",
    )
]


def fixture():
    files = {
        "p/ya.make": "PROTO_LIBRARY()\nSRCS(a.proto)\nUSE_COMMON_GOOGLE_APIS()\nEND()\n",
        "p/a.proto": 'syntax = "proto3";\nmessage A {}\n',
        "contrib/libs/googleapis-common-protos/ya.make": (
            f"LIBRARY()\n{NO_PLATFORM}SRCS(g.cpp)\nEND()\n"
        ),
        "contrib/libs/googleapis-common-protos/g.cpp": "int g;\n",
        "cpp/ya.make": f"PROGRAM()\n{NO_PLATFORM}SRCS(m.cpp)\nPEERDIR(p)\nEND()\n",
        "cpp/m.cpp": "int main(){return 0;}\n",
        "py3/ya.make": f"PY3_PROGRAM()\n{NO_PLATFORM}PEERDIR(p)\nEND()\n",
        "pylib/ya.make": f"PY3_LIBRARY()\n{NO_PLATFORM}PEERDIR(p)\nEND()\n",
        "py3t/ya.make": f"PY3_PROGRAM()\n{NO_PLATFORM}PEERDIR(pylib)\nEND()\n",
        "q/ya.make": "PROTO_LIBRARY()\nSRCS(b.proto)\nEXCLUDE_TAGS(CPP_PROTO)\nEND()\n",
        "q/b.proto": 'syntax = "proto3";\nmessage B {}\n',
        "pyq/ya.make": f"PY3_LIBRARY()\n{NO_PLATFORM}PEERDIR(q)\nEND()\n",
        "py3q/ya.make": f"PY3_PROGRAM()\n{NO_PLATFORM}PEERDIR(pyq)\nEND()\n",
        "d/ya.make": "PROTO_DESCRIPTIONS(descs)\nPEERDIR(p)\nEND()\n",
        "nested/deep/pp/ya.make": "PROTO_LIBRARY(named)\nSRCS(n.proto)\nEND()\n",
        "nested/deep/pp/n.proto": 'syntax = "proto3";\nmessage N {}\n',
        "d2/ya.make": "PROTO_DESCRIPTIONS(descs)\nPEERDIR(nested/deep/pp)\nEND()\n",
        "pyn/ya.make": f"PY3_PROGRAM()\n{NO_PLATFORM}PEERDIR(nested/deep/pp)\nEND()\n",
        "gr/ya.make": "PROTO_LIBRARY()\nSRCS(s.proto)\nGRPC()\nEND()\n",
        "gr/s.proto": 'syntax = "proto3";\nservice S {}\n',
        "pygr/ya.make": f"PY3_PROGRAM()\n{NO_PLATFORM}PEERDIR(gr)\nEND()\n",
        "ps/ya.make": f"PY3_LIBRARY()\n{NO_PLATFORM}PY_SRCS(m.py x.proto)\nEND()\n",
        "ps/m.py": "x = 1\n",
        "ps/x.proto": 'syntax = "proto3";\nmessage X {}\n',
        "pyps/ya.make": f"PY3_PROGRAM()\n{NO_PLATFORM}PEERDIR(ps)\nEND()\n",
        "ap/ya.make": f"PY3_LIBRARY()\n{NO_PLATFORM}ALL_PY_SRCS()\nEND()\n",
        "ap/m.py": "x = 1\n",
        "ap/n.py": "y = 2\n",
        "pyap/ya.make": f"PY3_PROGRAM()\n{NO_PLATFORM}PEERDIR(ap)\nEND()\n",
    }
    for path in ("contrib/libs/grpc", "contrib/python/grpcio", "contrib/python/protobuf"):
        name = path.split("/")[-1]
        files[f"{path}/ya.make"] = f"LIBRARY()\n{NO_PLATFORM}SRCS({name}.cpp)\nEND()\n"
        files[f"{path}/{name}.cpp"] = f"int {name};\n"
    for path in TOOLS:
        lib.tool_program(files, path, path.split("/")[-1])
    for path in LIBRARIES:
        files[f"{path}/ya.make"] = f"LIBRARY()\n{NO_PLATFORM}END()\n"
    for path in PLAIN_FILES:
        files[path] = "\n"
    return files


def make_with_env(files, target, extra_env):
    with tempfile.TemporaryDirectory(prefix="ay-link-proto-") as directory:
        root = Path(directory)
        (root / ".arcadia.root").touch()
        (root / "ya.conf").write_text(
            '[flags]\nOPENSOURCE = "yes"\n\n[host_platform_flags]\nOPENSOURCE = "yes"\n'
        )
        for relative, content in files.items():
            path = root / relative
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(content)
        env = {
            key: value
            for key, value in os.environ.items()
            if key not in lib.TOOLCHAIN_ENV_VARS
        }
        env.update(extra_env)
        result = lib.run(
            "make", "-j0", "-G", "--sandboxing",
            "--source-root", root,
            "--target-platform", "default-linux-aarch64",
            "--host-platform", "default-linux-x86_64",
            target,
            env=env,
        )
        return json.loads(result.stdout), result.stderr


def link_args(graph, output):
    node = lib.node_by_output(graph, output)
    return next(
        cmd["cmd_args"]
        for cmd in node["cmds"]
        if any(arg.endswith("link_exe.py") for arg in cmd["cmd_args"])
    )


def between(args, start, end):
    return args[args.index(start) + 1:args.index(end)]


GOOGLEAPIS = (
    "contrib/libs/googleapis-common-protos/"
    "libcontrib-libs-googleapis-common-protos.a"
)


class ProtoPeersTest(unittest.TestCase):
    def test_cpp_program_links_common_googleapis_before_proto(self):
        graph = lib.make(fixture(), "cpp")
        args = link_args(graph, "$(B)/cpp/cpp")
        self.assertEqual(
            between(args, "-Wl,--start-group", "-Wl,--end-group"),
            [GOOGLEAPIS, "p/libp.a"],
        )
        self.assertNotIn("--whole-archive-libs", args)
        self.assertEqual(between(args, "--ya-start-command-file", "--ya-end-command-file"), [])

    def test_python_programs_whole_archive_cpp_protos(self):
        for target in ("py3", "py3t"):
            with self.subTest(target=target):
                graph = lib.make(fixture(), target)
                args = link_args(graph, f"$(B)/{target}/{target}")
                self.assertEqual(
                    args[args.index("--whole-archive-libs"):args.index("--arch=LINUX")],
                    ["--whole-archive-libs", "p/libp.a"],
                )
                self.assertEqual(
                    between(args, "--ya-start-command-file", "--ya-end-command-file"),
                    ["p/libpy3p.global.a"],
                )
                self.assertIn(GOOGLEAPIS, between(args, "-Wl,--start-group", "-Wl,--end-group"))
                link = lib.node_by_output(graph, f"$(B)/{target}/{target}")
                self.assertIn("$(B)/p/libp.a", link["inputs"])
                self.assertIn("$(B)/p/libpy3p.global.a", link["inputs"])

    def test_python_only_proto_names_whole_archive_on_command_line_only(self):
        graph = lib.make(fixture(), "py3q")
        args = link_args(graph, "$(B)/py3q/py3q")
        self.assertEqual(
            args[args.index("--whole-archive-libs"):args.index("--arch=LINUX")],
            ["--whole-archive-libs", "q/libq.a"],
        )
        self.assertEqual(
            between(args, "--ya-start-command-file", "--ya-end-command-file"),
            ["q/libpy3q.global.a"],
        )
        link = lib.node_by_output(graph, "$(B)/py3q/py3q")
        self.assertNotIn("$(B)/q/libq.a", link["inputs"])
        produced = {out for node in graph["graph"] for out in node["outputs"]}
        self.assertNotIn("$(B)/q/libq.a", produced)
        self.assertNotIn("$(B)/q/b.pb.cc", produced)

    def test_proto_descriptions_merge_peer_descriptor_sets(self):
        graph = lib.make(fixture(), "d")
        per_file = lib.node_by_output(graph, "$(B)/p/a.proto.desc")
        self_desc = lib.node_by_output(graph, "$(B)/p/p.self.protodesc")
        merged = lib.node_by_output(graph, "$(B)/d/d.protodesc")
        for node in (per_file, self_desc, merged):
            self.assertEqual(node["kv"]["p"], "PD")
        self.assertEqual(merged["outputs"], ["$(B)/d/d.protodesc", "$(B)/d/d.tar"])
        self.assertEqual(
            merged["cmds"][0]["cmd_args"][1:],
            ["$(S)/build/scripts/merge_files.py", "$(B)/d/d.protodesc", "$(B)/p/p.self.protodesc"],
        )
        self.assertEqual(
            merged["cmds"][1]["cmd_args"][-3:],
            ["--output", "$(B)/d/d.tar", "p/p.self.protodesc"],
        )
        self.assertIn(self_desc["uid"], merged["deps"])

    def test_grpc_proto_adds_grpc_peers_for_cpp_and_python(self):
        graph = lib.make(fixture(), "pygr")
        cpp = lib.node_by_output(graph, "$(B)/gr/s.pb.h")
        self.assertEqual(
            cpp["outputs"],
            ["$(B)/gr/s.pb.h", "$(B)/gr/s.pb.cc", "$(B)/gr/s.grpc.pb.cc", "$(B)/gr/s.grpc.pb.h"],
        )
        python = lib.node_by_output(graph, "$(B)/gr/s__intpy3___pb2_grpc.py")
        self.assertEqual(python["kv"]["p"], "PB")
        group = between(link_args(graph, "$(B)/pygr/pygr"), "-Wl,--start-group", "-Wl,--end-group")
        self.assertLess(group.index("contrib/libs/grpc/libcontrib-libs-grpc.a"), group.index("gr/libgr.a"))
        self.assertIn("contrib/python/grpcio/libcontrib-python-grpcio.a", group)
        self.assertIn("contrib/python/protobuf/libcontrib-python-protobuf.a", group)

    def test_python_sources_with_protos_and_all_py_srcs(self):
        graph = lib.make(fixture(), "pyps")
        self.assertEqual(
            lib.node_by_output(graph, "$(B)/ps/x__intpy3___pb2.py")["kv"]["p"], "PB"
        )
        self.assertIn(
            "contrib/python/protobuf/libcontrib-python-protobuf.a",
            between(link_args(graph, "$(B)/pyps/pyps"), "-Wl,--start-group", "-Wl,--end-group"),
        )
        graph = lib.make(fixture(), "pyap")
        for name in ("m", "n"):
            with self.subTest(name=name):
                node = lib.node_by_output(graph, f"$(B)/ap/{name}.py.yapyc3")
                self.assertIn(f"$(S)/ap/{name}.py", node["inputs"])

    def test_python_proto_pipeline_passes_arena_ownership_audit(self):
        audited, stderr = make_with_env(fixture(), "py3", {"AY_DEBUG_OWNERSHIP": "1"})
        self.assertIn("ownership: 0 violating (field, site) pairs\n", stderr)
        self.assertEqual(audited, lib.make(fixture(), "py3"))
        compile_node = lib.node_by_output(audited, "$(B)/p/a__intpy3___pb2.py.b45u.yapyc3")
        self.assertEqual(compile_node["cmds"][0]["cmd_args"][1], "--slow-py3cc")

    def test_nested_and_named_proto_archive_names(self):
        graph = lib.make(fixture(), "d2")
        self_desc = lib.node_by_output(graph, "$(B)/nested/deep/pp/nested-deep-pp.self.protodesc")
        self.assertEqual(
            self_desc["outputs"],
            [
                "$(B)/nested/deep/pp/nested-deep-pp.self.protodesc",
                "$(B)/nested/deep/pp/nested-deep-pp.protosrc",
            ],
        )
        graph = lib.make(fixture(), "pyn")
        args = link_args(graph, "$(B)/pyn/pyn")
        self.assertEqual(
            args[args.index("--whole-archive-libs"):args.index("--arch=LINUX")],
            ["--whole-archive-libs", "nested/deep/pp/libnamed.a"],
        )
        self.assertEqual(
            between(args, "--ya-start-command-file", "--ya-end-command-file"),
            ["nested/deep/pp/libpy3named.global.a"],
        )


if __name__ == "__main__":
    unittest.main(verbosity=2)
