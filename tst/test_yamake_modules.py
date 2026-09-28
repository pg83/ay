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
PYTHON_PROGRAM_PEERS = [
    "contrib/libs/python",
    "contrib/tools/python3/Modules/_sqlite",
    "library/cpp/malloc/jemalloc",
    "library/python/import_tracing/constructor",
    "library/python/runtime_py3/main",
    "library/python/testing/import_test",
]


def ay_make(files, target="a", *args):
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


def stub_libraries(files, paths):
    for path in paths:
        files[f"{path}/ya.make"] = "LIBRARY()\n" + NO_PLATFORM + "END()\n"
    return files


def module_outputs(graph, prefix="$(B)/a/"):
    return sorted({
        (node["kv"]["p"], output)
        for node in graph["graph"]
        for output in node["outputs"]
        if output.startswith(prefix)
    })


def user_cflags(graph, output):
    args = lib.node_by_output(graph, output)["cmds"][0]["cmd_args"]
    start = args.index("-D__LONG_LONG_SUPPORTED") + 1
    return args[start:args.index("-UNDEBUG", start)]


class YaMakeModulesTest(unittest.TestCase):
    def make(self, files, target="a", *args):
        code, graph, stderr = ay_make(files, target, *args)
        self.assertEqual(code, 0, stderr)
        return graph

    def test_module_types_select_artifacts(self):
        cases = [
            ("PROGRAM()", [], [("CC", "$(B)/a/x.cpp.o"), ("LD", "$(B)/a/a")]),
            ("LIBRARY()", [], [("AR", "$(B)/a/liba.a"), ("CC", "$(B)/a/x.cpp.o")]),
            ("PY23_NATIVE_LIBRARY()", [], [("AR", "$(B)/a/libpy3ca.a"), ("CC", "$(B)/a/x.cpp.py3.o")]),
            ("PY3_LIBRARY()", ["contrib/libs/python"], [("AR", "$(B)/a/libpy3a.a"), ("CC", "$(B)/a/x.cpp.o")]),
            ("PY23_LIBRARY()", ["contrib/libs/python"], [("AR", "$(B)/a/libpy3a.a"), ("CC", "$(B)/a/x.cpp.py3.o")]),
            ("PY2_LIBRARY()", ["contrib/libs/python"], [("AR", "$(B)/a/libpy3a.a"), ("CC", "$(B)/a/x.cpp.o")]),
            ("PY2_PROGRAM()", ["contrib/libs/python", "library/python/import_tracing/constructor"], [("CC", "$(B)/a/x.cpp.o"), ("LD", "$(B)/a/a")]),
            ("PY3_PROGRAM_BIN()", PYTHON_PROGRAM_PEERS, [("CC", "$(B)/a/x.cpp.o"), ("LD", "$(B)/a/a")]),
            ("PY3_PROGRAM()", PYTHON_PROGRAM_PEERS, [("AR", "$(B)/a/libpy3a.a"), ("CC", "$(B)/a/x.cpp.o"), ("LD", "$(B)/a/a")]),
            ("YQL_UDF_YDB()", ["yql/essentials/public/udf", "yql/essentials/public/udf/support"], [("AR", "$(B)/a/liba.global.a"), ("CC", "$(B)/a/x.cpp.udfs.o")]),
            ("YQL_UDF_CONTRIB()", ["yql/essentials/public/udf", "yql/essentials/public/udf/support"], [("AR", "$(B)/a/liba.global.a"), ("CC", "$(B)/a/x.cpp.udfs.o")]),
            ("PROTO_LIBRARY()", [], [("AR", "$(B)/a/liba.a"), ("CC", "$(B)/a/x.cpp.o")]),
            ("PROTO_SCHEMA()", [], [("AR", "$(B)/a/liba.a"), ("CC", "$(B)/a/x.cpp.o")]),
            ("PROTO_DESCRIPTIONS()", [], [("PD", "$(B)/a/a.protodesc"), ("PD", "$(B)/a/a.tar")]),
            ("DLL()", [], [("AR", "$(B)/a/liba.a"), ("CC", "$(B)/a/x.cpp.o")]),
            ("SO_PROGRAM()", [], [("AR", "$(B)/a/liba.a"), ("CC", "$(B)/a/x.cpp.o")]),
            ("PACKAGE()", [], [("AR", "$(B)/a/liba.a"), ("CC", "$(B)/a/x.cpp.o")]),
            ("UNION()", [], [("AR", "$(B)/a/liba.a"), ("CC", "$(B)/a/x.cpp.o")]),
            ("FBS_LIBRARY()", [], [("AR", "$(B)/a/liba.a"), ("CC", "$(B)/a/x.cpp.o")]),
            ("RESOURCES_LIBRARY()", [], []),
        ]
        for declaration, stubs, expected in cases:
            with self.subTest(declaration=declaration):
                files = stub_libraries({
                    "a/ya.make": declaration + "\n" + NO_PLATFORM + "SRCS(x.cpp)\nEND()\n",
                    "a/x.cpp": "int x;\n",
                }, stubs)
                self.assertEqual(module_outputs(self.make(files)), expected)

    def test_unittest_for_links_the_tested_library(self):
        graph = self.make({
            "a/ya.make": "UNITTEST_FOR(u/./)\n" + NO_PLATFORM + "SRCS(x.cpp)\nEND()\n",
            "a/x.cpp": "int x;\n",
            "u/ya.make": "LIBRARY()\n" + NO_PLATFORM + "SRCS(u.cpp)\nEND()\n",
            "u/u.cpp": "int u;\n",
            "library/cpp/testing/unittest_main/ya.make": "LIBRARY()\n" + NO_PLATFORM + "SRCS(main.cpp)\nEND()\n",
            "library/cpp/testing/unittest_main/main.cpp": "int m;\n",
        })
        self.assertEqual(
            [path for path in lib.node_by_output(graph, "$(B)/a/a")["inputs"] if path.endswith(".a")],
            ["$(B)/u/libu.a", "$(B)/library/cpp/testing/unittest_main/libcpp-testing-unittest_main.a"],
        )

    def test_module_statement_binds_module_variables(self):
        body = (
            "CFLAGS(-DLANG=${MODULE_LANG} -DTAG=${MODULE_TAG})\n"
            "IF (GEN_PROTO)\nCFLAGS(-DGEN_PROTO)\nENDIF()\n"
            "SRCS(x.cpp)\nEND()\n"
        )
        cases = [
            ("LIBRARY()", ["-DLANG=CPP", "-DTAG=PY3"]),
            ("PROTO_LIBRARY()", ["-DLANG=CPP", "-DTAG=CPP_PROTO", "-DGEN_PROTO"]),
            ("PY23_NATIVE_LIBRARY()", ["-DLANG=CPP", "-DTAG=PY3"]),
        ]
        for declaration, expected in cases:
            with self.subTest(declaration=declaration):
                graph = self.make({
                    "a/ya.make": declaration + "\n" + NO_PLATFORM + body,
                    "a/x.cpp": "int x;\n",
                })
                output = next(o for kind, o in module_outputs(graph) if kind == "CC")
                self.assertEqual(user_cflags(graph, output), expected)

    def test_module_declaration_errors(self):
        cases = [
            ("LIBRARY()\nSRCS(x.cpp)\nPROGRAM()\nEND()\n", "gen: a declares multiple modules (LIBRARY and PROGRAM); only one is allowed"),
            ("SRCS(x.cpp)\n", "gen: a has no module declaration (PROGRAM/LIBRARY)"),
            ("DLL_TOOL()\nSRCS(x.cpp)\nEND()\n", "gen: a DLL requires EXPORTS_SCRIPT(...)"),
            ("DYNAMIC_LIBRARY()\nSRCS(x.cpp)\nEND()\n", "gen: a DYNAMIC_LIBRARY requires a basename argument"),
            ("PREBUILT_PROGRAM()\nEND()\n", "gen: a: PREBUILT_PROGRAM has no PRIMARY_OUTPUT/resource"),
        ]
        for text, expected in cases:
            with self.subTest(text=text):
                code, _, stderr = ay_make({"a/ya.make": text, "a/x.cpp": ""})
                self.assertEqual((code, stderr), (1, expected))


if __name__ == "__main__":
    unittest.main(verbosity=2)
