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
LIBRARIES = [
    "build/induced/by_bison",
    "build/platform/java/jdk/jdk17",
    "library/cpp/resource",
    "tools/enum_parser/enum_serialization_runtime",
]
TOOLS = [
    "contrib/tools/bison",
    "contrib/tools/flex-old",
    "contrib/tools/m4",
    "contrib/tools/ragel6",
    "tools/aux",
    "tools/bc",
    "tools/enum_parser/enum_parser",
    "tools/gen",
    "tools/lua",
    "tools/rescompiler",
    "tools/rescompressor",
    "tools/sc",
]


def tree(body, sources):
    files = {"a/ya.make": "LIBRARY()\n" + NO_PLATFORM + body + "\nEND()\n"}
    for source in sources:
        files[f"a/{source}"] = "\n"
    for path in LIBRARIES:
        files[f"{path}/ya.make"] = "LIBRARY()\n" + NO_PLATFORM + "END()\n"
    for path in TOOLS:
        lib.tool_program(files, path, path.rsplit("/", 1)[-1])
    return files


def ay_make(files, *args):
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
                *args, "a",
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


def module_nodes(graph):
    return [
        node for node in graph["graph"]
        if node["outputs"][0].startswith("$(B)/a/")
    ]


class YaMakeCodegenStatementsTest(unittest.TestCase):
    def make(self, body, sources=()):
        code, graph, stderr = ay_make(tree(body, sources), "-k")
        self.assertEqual(code, 0, stderr)
        return graph

    def test_run_program_sections(self):
        graph = self.make(
            "RUN_PROGRAM(tools/gen arg1 ${CURDIR}/in.txt IN in.txt IN_NOPARSE np.txt"
            " IN_DEPS dep.txt OUT out.cpp OUT_NOAUTO na.h STDOUT so.txt STDOUT ignored.txt"
            " CWD ${BINDIR} CWD ignored ENV A=1 B=2 OUTPUT_INCLUDES a/inc.h"
            " INDUCED_DEPS a/ind.h TOOL tools/aux)\n"
            "RUN_PY3_PROGRAM(tools/gen STDOUT_NOAUTO so2.cpp OUT py.cpp)",
            ["in.txt", "np.txt", "dep.txt", "inc.h", "ind.h"],
        )
        node = lib.node_by_output(graph, "$(B)/a/out.cpp")
        self.assertEqual(node["outputs"], ["$(B)/a/so.txt", "$(B)/a/out.cpp", "$(B)/a/na.h"])
        cmd = node["cmds"][0]
        self.assertEqual(cmd["cmd_args"], ["$(B)/tools/gen/gen", "arg1", "$(S)/a/in.txt"])
        self.assertEqual(cmd["cwd"], "$(B)/a")
        self.assertEqual(cmd["stdout"], "$(B)/a/so.txt")
        self.assertEqual({k: cmd["env"][k] for k in ("A", "B")}, {"A": "1", "B": "2"})
        for source in ("in.txt", "np.txt", "dep.txt", "inc.h", "ind.h"):
            self.assertIn(f"$(S)/a/{source}", node["inputs"])
        self.assertIn("$(B)/tools/aux/aux", node["inputs"])
        py3 = lib.node_by_output(graph, "$(B)/a/py.cpp")
        self.assertEqual(py3["outputs"], ["$(B)/a/so2.cpp", "$(B)/a/py.cpp"])

    def test_run_python_and_lua_sections(self):
        graph = self.make(
            "RUN_PYTHON3(gen.py arg IN in.txt OUT o.cpp OUT_NOAUTO n.h STDOUT s.txt"
            " STDOUT_NOAUTO ignored.txt CWD ${BINDIR} CWD ignored ENV X=1"
            " OUTPUT_INCLUDES a/inc.h TOOL tools/aux)\n"
            "RUN_LUA(gen.lua larg OUT l.cpp)",
            ["gen.py", "gen.lua", "in.txt", "inc.h"],
        )
        node = lib.node_by_output(graph, "$(B)/a/o.cpp")
        self.assertEqual(node["outputs"], ["$(B)/a/s.txt", "$(B)/a/o.cpp", "$(B)/a/n.h"])
        cmd = node["cmds"][0]
        self.assertEqual(cmd["cmd_args"][1:], ["$(S)/a/gen.py", "arg"])
        self.assertEqual((cmd["cwd"], cmd["stdout"], cmd["env"]["X"]), ("$(B)/a", "$(B)/a/s.txt", "1"))
        lua = lib.node_by_output(graph, "$(B)/a/l.cpp")
        self.assertIn("$(S)/a/gen.lua", lua["cmds"][0]["cmd_args"])
        self.assertIn("larg", lua["cmds"][0]["cmd_args"])

    def test_split_and_base_codegen_arguments(self):
        graph = self.make(
            "SPLIT_CODEGEN(tools/sc pre OUT_NUM 3 opt1 OUTPUT_INCLUDES a/inc.h OUT_NUM bad)\n"
            "BASE_CODEGEN(tools/bc base opt1 opt2)\nSRCS(${BINDIR}/base.cpp)",
            ["pre.in", "base.in", "inc.h"],
        )
        split = lib.only_node_by_kind(graph, "SC")
        self.assertEqual(split["outputs"], [
            "$(B)/a/pre.0.cpp", "$(B)/a/pre.1.cpp", "$(B)/a/pre.2.cpp",
            "$(B)/a/pre.cpp", "$(B)/a/pre.h",
        ])
        self.assertEqual(split["cmds"][0]["cmd_args"][-1], "opt1")
        base = lib.only_node_by_kind(graph, "BC")
        self.assertEqual(base["cmds"][0]["cmd_args"], [
            "$(B)/tools/bc/bc", "$(S)/a/base.in", "$(B)/a/base.cpp", "$(B)/a/base.h", "opt1", "opt2",
        ])

    def test_antlr_statements(self):
        graph = self.make(
            "RUN_ANTLR4_CPP(G.g4 -package p VISITOR NO_LISTENER LISTENER OUTPUT_INCLUDES a/inc.h"
            " IN x.txt OUT y.txt)\n"
            "RUN_ANTLR4_CPP_SPLIT(L.g4 P.g4 VISITOR LISTENER NO_LISTENER OUTPUT_INCLUDES a/inc.h"
            " IN x.txt TOOL t)\n"
            "RUN_ANTLR(G.g g -o x IN G.g IN_NOPARSE np OUT G.cpp OUT_NOAUTO G.h CWD ${BINDIR}"
            " CWD ignored OUTPUT_INCLUDES a/inc.h GRAMMAR_FILES q)",
            ["G.g4", "L.g4", "P.g4", "G.g", "np", "inc.h", "x.txt"],
        )
        java = {node["outputs"][0]: node for node in graph["graph"] if node["kv"]["p"] == "JV"}
        self.assertEqual({output: node["outputs"] for output, node in java.items()}, {
            "$(B)/a/GLexer.cpp": [
                "$(B)/a/GLexer.cpp", "$(B)/a/GLexer.h", "$(B)/a/GParser.cpp", "$(B)/a/GParser.h",
                "$(B)/a/GVisitor.h", "$(B)/a/GBaseVisitor.h",
            ],
            "$(B)/a/L.cpp": [
                "$(B)/a/L.cpp", "$(B)/a/L.h", "$(B)/a/P.cpp", "$(B)/a/P.h",
                "$(B)/a/PVisitor.h", "$(B)/a/PBaseVisitor.h",
            ],
            "$(B)/a/G.cpp": ["$(B)/a/G.cpp", "$(B)/a/G.h"],
        })
        self.assertEqual(
            java["$(B)/a/GLexer.cpp"]["cmds"][0]["cmd_args"][-5:],
            ["$(B)/a", "-visitor", "-listener", "-package", "p"],
        )
        self.assertEqual(java["$(B)/a/L.cpp"]["cmds"][0]["cmd_args"][-2:], ["-visitor", "-listener"])
        self.assertEqual(java["$(B)/a/G.cpp"]["cmds"][0]["cmd_args"][-4:], ["$(S)/a/G.g", "g", "-o", "x"])
        self.assertEqual(java["$(B)/a/G.cpp"]["cmds"][0]["cwd"], "$(B)/a")
        self.assertIn("$(S)/a/inc.h", lib.node_by_output(graph, "$(B)/a/G.cpp.o")["inputs"])

    def test_from_sandbox_sections(self):
        graph = self.make(
            "FROM_SANDBOX(FILE 123 AUTOUPDATED x PREFIX pre SBR sb OUT out.o OUT_NOAUTO na.h"
            " OUTPUT_INCLUDES a/inc.h RENAME r1 r2 INDUCED_DEPS skip EXECUTABLE 456)\n"
            "FROM_SANDBOX(789 OUT lib.a)",
            ["inc.h"],
        )
        first = lib.node_by_output(graph, "$(B)/a/out.o")
        self.assertEqual(first["outputs"], ["$(B)/a/out.o", "$(B)/a/na.h"])
        self.assertEqual(first["cmds"][0]["cmd_args"][5:], [
            "--resource-id", "123", "--copy-to-dir", "pre", "--rename", "r1",
            "--rename", "r2", "--executable", "--", "out.o", "na.h", "--ya-end-command-file",
        ])
        second = lib.node_by_output(graph, "$(B)/a/lib.a")
        self.assertEqual(second["cmds"][0]["cmd_args"][5:9], ["--resource-id", "789", "--untar-to", "."])

    def test_resource_file_statements(self):
        cases = [
            ("RESOURCE_FILES(PREFIX p/ r1.txt DEST d r2.txt)", [
                "resfs/src/resfs/file/p/r1.txt=a/r1.txt", "resfs/src/resfs/file/d=a/r2.txt",
            ]),
            ("ALL_RESOURCE_FILES(txt PREFIX pre/ sub)", ["resfs/src/resfs/file/pre/sub/a.txt=a/sub/a.txt"]),
            ("ALL_RESOURCE_FILES_FROM_DIRS(PREFIX pre/ sub)", [
                "resfs/src/resfs/file/pre/sub/a.txt=a/sub/a.txt",
                "resfs/src/resfs/file/pre/sub/b.bin=a/sub/b.bin",
            ]),
        ]
        for body, expected in cases:
            with self.subTest(body=body):
                graph = self.make(body, ["r1.txt", "r2.txt", "sub/a.txt", "sub/b.bin", "sub/c/d.txt"])
                node = next(n for n in module_nodes(graph) if n["kv"]["p"] == "PY")
                args = node["cmds"][0]["cmd_args"]
                self.assertEqual(args[args.index("--kvs") + 1:], expected)

    def test_generated_headers_and_sources(self):
        graph = self.make(
            "GENERATE_ENUM_SERIALIZATION_WITH_HEADER(e1.h)\n"
            "GENERATE_ENUM_SERIALIZATION_NOUTF(e2.h)\n"
            "CPP_ENUMS_SERIALIZATION(NAMESPACE ns e3.h e4.h)\n"
            "CONFIGURE_FILE(c.h.in c.h)\n"
            "CONFIGURE_FILE(${CURDIR}/d.cpp.in sub/d.cpp)\n"
            "CONFIGURE_FILE(e.in ${ARCADIA_BUILD_ROOT}/gen/e.txt)\n"
            "CREATE_BUILDINFO_FOR(bi.h)\n"
            "SET(RAGEL6_FLAGS -G2 -x)\n"
            "SRCS(x.cpp ${BINDIR}/sub/d.cpp p.y l.l f.lex r.rl6)",
            ["e1.h", "e2.h", "e3.h", "e4.h", "c.h.in", "d.cpp.in", "e.in", "x.cpp", "p.y", "l.l", "f.lex", "r.rl6"],
        )
        self.assertEqual([node["outputs"] for node in graph["graph"] if node["kv"]["p"] == "EN"], [
            ["$(B)/a/e1.h_serialized.cpp", "$(B)/a/e1.h_serialized.h"],
            ["$(B)/a/e2.h_serialized.cpp"],
            ["$(B)/a/e3.h_serialized.cpp", "$(B)/a/e3.h_serialized.h"],
            ["$(B)/a/e4.h_serialized.cpp", "$(B)/a/e4.h_serialized.h"],
        ])
        includes = [
            arg for arg in lib.node_by_output(graph, "$(B)/a/x.cpp.o")["cmds"][0]["cmd_args"]
            if arg.startswith("-I")
        ]
        self.assertEqual(includes, [
            "-I$(B)", "-I$(S)", "-I$(B)/a", "-I$(S)/contrib/tools/flex-old", "-I$(B)/a/sub", "-I$(B)/gen",
        ])
        self.assertEqual(lib.node_by_output(graph, "$(B)/a/p.y.cpp")["outputs"], ["$(B)/a/p.h", "$(B)/a/p.y.cpp"])
        self.assertEqual(
            lib.node_by_output(graph, "$(B)/a/r.rl6.cpp")["cmds"][0]["cmd_args"][1:4],
            ["-G2", "-x", "-L"],
        )


if __name__ == "__main__":
    unittest.main(verbosity=2)
