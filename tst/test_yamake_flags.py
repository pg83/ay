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


def module(kind, body):
    return f"{kind}()\n" + NO_PLATFORM + body + "\nEND()\n"


def args_of(graph, output):
    return lib.node_by_output(graph, output)["cmds"][0]["cmd_args"]


def includes(graph, output):
    return [arg for arg in args_of(graph, output) if arg.startswith("-I")]


def user_defines(graph, output):
    return [
        arg for arg in args_of(graph, output)
        if arg.startswith("-D") and arg[2:3].isupper() and "=" not in arg
    ]


class YaMakeFlagsTest(unittest.TestCase):
    def make(self, files, target, *args):
        code, graph, stderr = ay_make(files, target, *args)
        self.assertEqual(code, 0, stderr)
        return graph, stderr

    def test_addincl_scopes_propagate_by_depth(self):
        graph, stderr = self.make({
            "top/ya.make": module("PROGRAM", "PEERDIR(mid)\nSRCS(main.cpp)"),
            "top/main.cpp": "",
            "mid/ya.make": module("LIBRARY", "PEERDIR(l ${UNSET_PEER})\nSRCS(mid.cpp)"),
            "mid/mid.cpp": "",
            "l/ya.make": module("LIBRARY", (
                "ADDINCL(GLOBAL l/g ONE_LEVEL l/one l/own GLOBAL l/missing"
                " ${ARCADIA_BUILD_ROOT}/gen $S/unexpanded GLOBAL FOR other l/x"
                " FOR other2 l/y GLOBAL FOR proto l/pr GLOBAL FOR cython l/gcy"
                " FOR cython l/cy GLOBAL)\n"
                "SRCS(l.cpp)"
            )),
            "l/l.cpp": "",
            "l/g/x.h": "", "l/one/x.h": "", "l/own/x.h": "", "l/x/x.h": "",
            "l/y/y.h": "", "l/pr/x.proto": "", "l/gcy/x.pxd": "", "l/cy/x.pxd": "",
        }, "top", "-k")
        self.assertEqual(
            stderr, "missing-addincl: l: ADDINCL to non existent source directory l/missing",
        )
        self.assertEqual(includes(graph, "$(B)/l/l.cpp.o"), [
            "-I$(B)", "-I$(S)", "-I$(S)/l/g", "-I$(S)/l/one", "-I$(S)/l/own",
            "-I$(B)/gen", "-I$(S)/$S/unexpanded", "-I$(S)/l/x", "-I$(S)/l/y",
        ])
        self.assertEqual(includes(graph, "$(B)/mid/mid.cpp.o"), [
            "-I$(B)", "-I$(S)", "-I$(S)/l/g", "-I$(S)/l/one", "-I$(S)/l/x",
        ])
        self.assertEqual(includes(graph, "$(B)/top/main.cpp.o"), [
            "-I$(B)", "-I$(S)", "-I$(S)/l/g", "-I$(S)/l/x",
        ])

    def test_self_and_asm_include_dirs(self):
        files = {
            "p/ya.make": module("PROGRAM", "PEERDIR(ADDINCL l GLOBAL m)\nSRCS(main.cpp)"),
            "p/main.cpp": "",
            "l/ya.make": module("LIBRARY", (
                "ADDINCLSELF()\nADDINCLSELF(FOR asm)\nADDINCLSELF(FOR cython)\n"
                "ADDINCL(FOR asm l/asm)\nSRCS(s.asm l.cpp)"
            )),
            "l/s.asm": "", "l/l.cpp": "", "l/asm/x.inc": "",
            "m/ya.make": module("LIBRARY", "SRCS(m.cpp)"),
            "m/m.cpp": "",
        }
        lib.tool_program(files, "contrib/tools/yasm", "yasm")
        graph, _ = self.make(files, "p")
        asm = args_of(graph, "$(B)/l/s.o")
        self.assertEqual(
            [asm[i + 1] for i, arg in enumerate(asm) if arg == "-I"],
            ["$(B)", "$(S)", "$(S)/l", "$(S)/l/asm"],
        )
        self.assertEqual(includes(graph, "$(B)/l/l.cpp.o"), ["-I$(B)", "-I$(S)", "-I$(S)/l"])
        self.assertEqual(includes(graph, "$(B)/p/main.cpp.o"), ["-I$(B)", "-I$(S)", "-I$(S)/l"])
        link = lib.node_by_output(graph, "$(B)/p/p")["cmds"][-2]["cmd_args"]
        self.assertEqual(
            link[link.index("-Wl,--start-group") + 1:link.index("-Wl,--end-group")],
            ["l/libl.a", "m/libm.a"],
        )

    def test_compiler_and_linker_flag_scopes(self):
        graph, _ = self.make({
            "p/ya.make": module("PROGRAM", (
                "PEERDIR(l)\nCFLAGS(-DPROG)\nCXXFLAGS(-DPROGCXX)\n"
                "CONLYFLAGS(-DPROGC)\nLDFLAGS(-lfoo -Wl,--bar)\nSRCS(main.cpp c.c)"
            )),
            "p/main.cpp": "", "p/c.c": "",
            "l/ya.make": module("LIBRARY", (
                "CFLAGS(GLOBAL -DLG -DLOWN GLOBAL \"-DLQ=\\\"q\\\"\")\n"
                "CXXFLAGS(GLOBAL -DLGCXX -DLOWNCXX)\n"
                "CONLYFLAGS(GLOBAL -DLGC -DLOWNC)\nSRCS(l.cpp l.c)"
            )),
            "l/l.cpp": "", "l/l.c": "",
        }, "p")
        self.assertEqual(user_defines(graph, "$(B)/l/l.cpp.o"), [
            "-DLOWN", "-DLG", "-DLOWNCXX", "-DLGCXX", "-DLGCXX",
        ])
        self.assertEqual(user_defines(graph, "$(B)/l/l.c.o"), [
            "-DLOWN", "-DLG", "-DLOWNC",
        ])
        self.assertIn("-DLQ=\"q\"", args_of(graph, "$(B)/l/l.cpp.o"))
        self.assertIn("-DLQ=\"q\"", args_of(graph, "$(B)/p/main.cpp.o"))
        self.assertEqual(user_defines(graph, "$(B)/p/main.cpp.o"), [
            "-DPROG", "-DLG", "-DPROGCXX", "-DLGCXX", "-DLGCXX",
        ])
        self.assertEqual(user_defines(graph, "$(B)/p/c.c.o"), [
            "-DPROG", "-DLG", "-DLGC", "-DPROGC",
        ])
        link = lib.node_by_output(graph, "$(B)/p/p")["cmds"][-2]["cmd_args"]
        self.assertEqual(link[link.index("-lfoo") + 1], "-Wl,--bar")

    def test_source_lists(self):
        graph, _ = self.make({
            "p/ya.make": module("PROGRAM", "PEERDIR(l)\nSRCS(main.cpp)"),
            "p/main.cpp": "",
            "l/ya.make": module("LIBRARY", (
                "SRCDIR(l/extra)\n"
                "SRCS(GLOBAL g.cpp e.cpp ${UNSET}/u.cpp)\n"
                "GLOBAL_SRCS(gs.cpp)\n"
                "JOIN_SRCS(all.cpp j1.cpp j2.cpp)"
            )),
            "l/g.cpp": "", "l/extra/e.cpp": "", "l/gs.cpp": "",
            "l/j1.cpp": "", "l/j2.cpp": "",
        }, "p")
        self.assertEqual(
            lib.node_by_output(graph, "$(B)/l/all.cpp")["inputs"],
            ["$(S)/l/j1.cpp", "$(S)/l/j2.cpp"],
        )
        self.assertEqual(
            lib.node_by_output(graph, "$(B)/l/libl.a")["inputs"][:-1],
            ["$(B)/l/_/extra/e.cpp.o", "$(B)/l/all.cpp.o"],
        )
        self.assertEqual(
            lib.node_by_output(graph, "$(B)/l/libl.global.a")["inputs"][:-1],
            ["$(B)/l/g.cpp.o", "$(B)/l/gs.cpp.o"],
        )
        self.assertEqual(
            lib.node_by_output(graph, "$(B)/l/_/extra/e.cpp.o")["inputs"],
            ["$(S)/l/extra/e.cpp"],
        )


if __name__ == "__main__":
    unittest.main(verbosity=2)
