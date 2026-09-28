import json
import os
import re
import subprocess
import tempfile
import unittest
from pathlib import Path

import lib


ANSI = re.compile(r"\x1b\[[0-9;]*m")
NO_PLATFORM = "NO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
PYTHON_LIBRARIES = [
    "contrib/libs/protobuf",
    "contrib/libs/python",
    "contrib/python/protobuf",
    "contrib/tools/python3/Modules/_sqlite",
    "library/cpp/malloc/jemalloc",
    "library/cpp/resource",
    "library/python/import_tracing/constructor",
    "library/python/runtime_py3/main",
    "library/python/testing/import_test",
]
PYTHON_TOOLS = [
    "contrib/python/mypy-protobuf/bin/protoc-gen-mypy",
    "contrib/tools/protoc",
    "contrib/tools/protoc/plugins/cpp_styleguide",
    "contrib/tools/swig",
    "tools/archiver",
    "tools/py3cc",
    "tools/py3cc/slow",
    "tools/rescompiler",
    "tools/rescompressor",
]


def module(kind, body):
    return f"{kind}()\n" + NO_PLATFORM + body + "\nEND()\n"


def stubs(files, libraries=(), tools=()):
    for path in libraries:
        files[f"{path}/ya.make"] = module("LIBRARY", "")
    for path in tools:
        lib.tool_program(files, path, path.rsplit("/", 1)[-1])
    return files


def ay_make(files, target, *args):
    with tempfile.TemporaryDirectory(prefix="ay-yamake-test-") as directory:
        root = Path(directory)
        (root / ".arcadia.root").touch()
        (root / "ya.conf").write_text(
            '[flags]\nOPENSOURCE = "yes"\n\n'
            '[host_platform_flags]\nOPENSOURCE = "yes"\n'
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
        result = subprocess.run(
            [
                str(lib.AY), "make", "-j0", "-G", "--sandboxing",
                "--source-root", str(root),
                "--target-platform", "default-linux-aarch64",
                "--host-platform", "default-linux-x86_64",
                *args, target,
            ],
            env=env,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
            timeout=30,
            check=False,
        )
        graph = json.loads(result.stdout) if result.returncode == 0 else None
        return result.returncode, graph, ANSI.sub("", result.stderr).strip()


def outputs_under(graph, prefix):
    return sorted(
        (node["kv"]["p"], node["outputs"][0])
        for node in graph["graph"]
        if node["outputs"][0].startswith(prefix)
    )


class YaMakeModuleDataTest(unittest.TestCase):
    def make(self, files, target="a", *args):
        code, graph, stderr = ay_make(files, target, *args)
        self.assertEqual(code, 0, stderr)
        return graph

    def test_python_instances_of_proto_libraries(self):
        python_half = [
            ("AR", "$(B)/p/libpy3p.global.a"),
            ("PB", "$(B)/p/m__intpy3___pb2.py"),
            ("PY", "$(B)/p/m__intpy3___pb2.py.b45u.yapyc3"),
        ]
        cpp_half = [("AR", "$(B)/p/libp.a"), ("CC", "$(B)/p/m.pb.cc.o"), ("PB", "$(B)/p/m.pb.h")]
        raw_resources = {}
        cases = [
            ("PROTO_LIBRARY", "EXCLUDE_TAGS(CPP_PROTO)", python_half),
            ("PROTO_LIBRARY", "", sorted(python_half + cpp_half)),
            ("PROTO_SCHEMA", "", sorted(python_half + cpp_half)),
        ]
        for kind, body, expected in cases:
            with self.subTest(kind=kind, body=body):
                files = stubs({
                    "a/ya.make": module("PY3_PROGRAM", "PEERDIR(p)\nPY_SRCS(x.py)"),
                    "a/x.py": "\n",
                    "p/ya.make": module(kind, body + "\nSRCS(m.proto)"),
                    "p/m.proto": 'syntax = "proto3";\n',
                }, PYTHON_LIBRARIES, PYTHON_TOOLS)
                graph = self.make(files, "a", "-k")
                produced = outputs_under(graph, "$(B)/p/")
                raw = [output for kind_, output in produced if output.endswith("_raw.auxcpp")]
                self.assertEqual(len(raw), 1)
                raw_resources[kind, body] = raw[0]
                self.assertEqual([item for item in produced if "_raw.auxcpp" not in item[1]], expected)
        self.assertEqual(raw_resources["PROTO_LIBRARY", ""], raw_resources["PROTO_LIBRARY", "EXCLUDE_TAGS(CPP_PROTO)"])
        self.assertNotEqual(raw_resources["PROTO_LIBRARY", ""], raw_resources["PROTO_SCHEMA", ""])

    def test_archiver_plugin(self):
        graph = self.make({
            "a/ya.make": module("LIBRARY", "AR_PLUGIN(plug)\nSRCS(x.cpp)"),
            "a/x.cpp": "int x;\n",
            "a/plug.pyplugin": "\n",
        })
        archive = lib.node_by_output(graph, "$(B)/a/liba.a")
        args = archive["cmds"][0]["cmd_args"]
        self.assertEqual(args[args.index("--plugin") + 1], "$(S)/a/plug.pyplugin")
        self.assertIn("$(S)/a/plug.pyplugin", archive["inputs"])

    def test_luajit_archive_and_flatbuffers_sources(self):
        files = stubs({
            "a/ya.make": module("LIBRARY", "LJ_21_ARCHIVE(a.lua b.txt c.lua)\nSRCS(x.cpp)"),
            "a/x.cpp": "int x;\n", "a/a.lua": "\n", "a/c.lua": "\n",
        }, tools=["contrib/libs/luajit_21/compiler", "tools/archiver"])
        graph = self.make(files)
        self.assertEqual(
            [output for kind, output in outputs_under(graph, "$(B)/a/") if kind == "LJ"],
            ["$(B)/a/a.raw", "$(B)/a/c.raw"],
        )
        self.assertIn("-I$(B)/a", lib.node_by_output(graph, "$(B)/a/x.cpp.o")["cmds"][0]["cmd_args"])

        files = stubs({
            "a/ya.make": module("LIBRARY", "SRCS(s.fbs t.fbs64)"),
            "a/s.fbs": "table S {}\n", "a/t.fbs64": "table T {}\n",
        }, ["contrib/libs/flatbuffers", "contrib/libs/flatbuffers64"], ["contrib/libs/flatbuffers/flatc", "contrib/libs/flatbuffers64/flatc"])
        graph = self.make(files, "a", "-k")
        self.assertEqual(lib.node_by_output(graph, "$(B)/a/s.fbs.h")["outputs"], [
            "$(B)/a/s.fbs.h", "$(B)/a/s.fbs.cpp", "$(B)/a/s.bfbs",
        ])
        self.assertEqual(lib.node_by_output(graph, "$(B)/a/t.fbs64.h")["outputs"], [
            "$(B)/a/t.fbs64.h", "$(B)/a/t.fbs64.cpp", "$(B)/a/t.bfbs64",
        ])

    def test_copy_auto_as_first_generated_source(self):
        graph = self.make({
            "a/ya.make": module("LIBRARY", "COPY(AUTO FROM gen f.cpp)"),
            "a/gen/f.cpp": "int f;\n",
        })
        self.assertEqual(outputs_under(graph, "$(B)/a/"), [
            ("AR", "$(B)/a/liba.a"), ("CC", "$(B)/a/f.cpp.o"), ("CP", "$(B)/a/f.cpp"),
        ])

    def test_induced_deps_of_a_proto_plugin_tool(self):
        cases = [
            ("INDUCED_DEPS(h+cpp inc/ind.h)", True),
            ("INDUCED_DEPS(cpp inc/ind.h)", True),
            ("INDUCED_DEPS(h inc/ind.h)", False),
            ("INDUCED_DEPS(h)", False),
        ]
        for induced, reaches_sources in cases:
            with self.subTest(induced=induced):
                files = stubs({
                    "a/ya.make": module("PROTO_LIBRARY", "CPP_PROTO_PLUGIN2(p2 tools/p2 .p2.h .p2.cpp)\nSRCS(x.proto)"),
                    "a/x.proto": 'syntax = "proto3";\n',
                    "inc/ind.h": "\n",
                    "tools/p2/ya.make": module("PROGRAM", induced + "\nSRCS(main.cpp)"),
                    "tools/p2/main.cpp": "int main(){return 0;}\n",
                }, ["contrib/libs/protobuf"], ["contrib/tools/protoc", "contrib/tools/protoc/plugins/cpp_styleguide"])
                graph = self.make(files, "a", "-k")
                for output in ("$(B)/a/x.pb.cc.o", "$(B)/a/x.p2.cpp.o"):
                    self.assertEqual(
                        "$(S)/inc/ind.h" in lib.node_by_output(graph, output)["inputs"], reaches_sources,
                    )

    def test_go_modules(self):
        go_libraries = [
            "build/external_resources/go_tools",
            "build/external_resources/yolint",
            "build/internal/platform/clang_toolchain_info",
            "contrib/go/_std_1.26/src/runtime",
            "contrib/go/_std_1.26/src/runtime/cgo",
            "contrib/go/_std_1.26/src/syscall",
            "library/go/core/buildinfo",
        ]
        files = stubs({
            "a/ya.make": "GO_LIBRARY()\nSRCS(a.go)\nCGO_SRCS(c.go)\nCGO_LDFLAGS(-lfoo)\nCGO_CFLAGS(-DCGO)\nEND()\n",
            "a/a.go": "package a\n", "a/c.go": "package a\n",
        }, go_libraries)
        graph = self.make(files, "a", "-k")
        cgo = lib.node_by_output(graph, "$(B)/a/c.cgo1.go")
        self.assertEqual(cgo["outputs"], [
            "$(B)/a/c.cgo1.go", "$(B)/a/c.cgo2.c", "$(B)/a/_cgo_export.h", "$(B)/a/_cgo_export.c",
            "$(B)/a/_cgo_gotypes.go", "$(B)/a/_cgo_main.c",
        ])
        self.assertIn("-DCGO", lib.node_by_output(graph, "$(B)/a/c.cgo2.c.o")["cmds"][0]["cmd_args"])

        files = stubs({
            "a/ya.make": "GO_PROGRAM()\nSRCS(main.go)\nEND()\n",
            "a/main.go": "package main\n",
        }, go_libraries)
        for peer in ("build/platform/lld", "build/cow/on", "contrib/libs/linux-headers", "util"):
            files[f"{peer}/ya.make"] = "LIBRARY()\nNO_PLATFORM()\nSRCS(stub.cpp)\nEND()\n"
            files[f"{peer}/stub.cpp"] = "int stub;\n"
        graph = self.make(files, "a", "-k")
        self.assertEqual(
            [path for path in lib.node_by_output(graph, "$(B)/a/a")["inputs"] if path.endswith(".a")],
            [
                "$(B)/build/platform/lld/libbuild-platform-lld.a",
                "$(B)/contrib/libs/linux-headers/libcontrib-libs-linux-headers.a",
                "$(B)/build/cow/on/libbuild-cow-on.a",
            ],
        )


if __name__ == "__main__":
    unittest.main(verbosity=2)
