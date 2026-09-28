import unittest

import lib


NO_PLATFORM = "NO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
TOOLS = [
    "contrib/python/mypy-protobuf/bin/protoc-gen-mypy",
    "contrib/tools/protoc",
    "contrib/tools/protoc/plugins/cpp_styleguide",
    "tools/py3cc",
    "tools/py3cc/slow",
    "tools/rescompiler",
]
LIBRARIES = [
    "contrib/libs/protobuf",
    "contrib/libs/python",
    "contrib/python/protobuf",
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
    }
    for path in TOOLS:
        lib.tool_program(files, path, path.split("/")[-1])
    for path in LIBRARIES:
        files[f"{path}/ya.make"] = f"LIBRARY()\n{NO_PLATFORM}END()\n"
    for path in PLAIN_FILES:
        files[path] = "\n"
    return files


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


if __name__ == "__main__":
    unittest.main(verbosity=2)
