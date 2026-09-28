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


def module(kind, body):
    return f"{kind}()\n" + NO_PLATFORM + body + "\nEND()\n"


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


def includes(graph, output):
    return [arg for arg in lib.node_by_output(graph, output)["cmds"][0]["cmd_args"] if arg.startswith("-I")]


class YaMakePathsTest(unittest.TestCase):
    def make(self, files, target="a", *args):
        code, graph, stderr = ay_make(files, target, *args)
        self.assertEqual(code, 0, stderr)
        return graph, stderr

    def test_copy_file_sources_and_output_includes(self):
        body = (
            "SET(VS ${ARCADIA_ROOT}/other/s0.cpp)\n"
            "SET(VH ${ARCADIA_ROOT}/other/h0.h)\n"
            "COPY_FILE(AUTO ${VS} d0.cpp OUTPUT_INCLUDES ${VH} ${ARCADIA_ROOT}/other/h1.h"
            " ${ARCADIA_BUILD_ROOT}/gen/h2.h ${BINDIR}/h3.h plain/h4.h)\n"
            "COPY_FILE(AUTO ${ARCADIA_ROOT}/other/s1.cpp d1.cpp)\n"
            "COPY_FILE(AUTO ${CURDIR}/s2.cpp d2.cpp)\n"
            "COPY_FILE(AUTO ./s3.cpp d3.cpp)\n"
            "COPY_FILE(AUTO sub/../s4.cpp d4.cpp)\n"
            "COPY_FILE(AUTO a/s5.cpp d5.cpp)\n"
            "COPY_FILE(AUTO ../other/s6.cpp d6.cpp)\n"
            "COPY_FILE(AUTO other/s7.cpp d7.cpp)\n"
            "COPY_FILE(AUTO missing.cpp d8.cpp)\n"
            "COPY_FILE(AUTO ${ARCADIA_BUILD_ROOT}/gen/s9.cpp d9.cpp)\n"
            "COPY_FILE(AUTO ${BINDIR}/s10.cpp d10.cpp)"
        )
        files = {"a/ya.make": module("LIBRARY", body), "a/plain/h4.h": ""}
        for source in ("other/s0.cpp", "other/h0.h", "other/h1.h", "other/s1.cpp", "a/s2.cpp", "a/s3.cpp",
                       "a/s4.cpp", "a/s5.cpp", "other/s6.cpp", "other/s7.cpp"):
            files[source] = "\n"
        graph, _ = self.make(files, "a", "-k")
        copies = {
            node["outputs"][0][len("$(B)/a/"):]: node["cmds"][0]["cmd_args"][3]
            for node in graph["graph"] if node["kv"]["p"] == "CP" and node["outputs"][0].startswith("$(B)/a/")
        }
        self.assertEqual(copies, {
            "d0.cpp": "$(S)/other/s0.cpp",
            "d1.cpp": "$(S)/other/s1.cpp",
            "d2.cpp": "$(S)/a/s2.cpp",
            "d3.cpp": "$(S)/a/s3.cpp",
            "d4.cpp": "$(S)/a/s4.cpp",
            "d5.cpp": "$(S)/a/s5.cpp",
            "d6.cpp": "$(S)/other/s6.cpp",
            "d7.cpp": "$(S)/other/s7.cpp",
            "d8.cpp": "$(S)/a/missing.cpp",
            "d9.cpp": "$(B)/gen/s9.cpp",
            "d10.cpp": "$(B)/a/s10.cpp",
        })
        self.assertEqual(
            lib.node_by_output(graph, "$(B)/a/d0.cpp")["inputs"],
            ["$(S)/other/s0.cpp", "$(S)/a/plain/h4.h", "$(S)/other/h0.h", "$(S)/other/h1.h"],
        )

    def test_source_path_forms(self):
        graph, _ = self.make({
            "a/ya.make": module("LIBRARY", (
                "SRCDIR(a/extra)\nSRCS(./e.cpp sub/../f.cpp ./g.cpp missing.cpp)\n"
                "CONFIGURE_FILE(r.h.in ${ARCADIA_BUILD_ROOT}/r.h)\n"
                "ADDINCL(\"\")"
            )),
            "a/extra/e.cpp": "", "a/f.cpp": "", "a/g.cpp": "", "a/r.h.in": "",
        })
        self.assertEqual({
            node["outputs"][0]: node["inputs"]
            for node in graph["graph"] if node["kv"]["p"] == "CC"
        }, {
            "$(B)/a/_/extra/e.cpp.o": ["$(S)/a/extra/e.cpp"],
            "$(B)/a/f.cpp.o": ["$(S)/a/f.cpp"],
            "$(B)/a/g.cpp.o": ["$(S)/a/g.cpp"],
            "$(B)/a/missing.cpp.o": ["$(S)/a/missing.cpp"],
        })
        self.assertEqual(includes(graph, "$(B)/a/f.cpp.o"), ["-I$(B)", "-I$(S)", "-I$(B)/a", "-I$(S)"])

    def test_configure_file_variables_lose_surrounding_quotes(self):
        graph, _ = self.make({
            "a/ya.make": module("LIBRARY", (
                "SET(Q1 \"\\\"quoted\\\"\")\nSET(Q2 '\"plain\"')\nSET(Q3 bare)\n"
                "DEFAULT(Q4 '\"dflt\"')\nCONFIGURE_FILE(c.h.in c.h)\nSRCS(x.cpp)"
            )),
            "a/c.h.in": "@Q1@ @Q2@ @Q3@ @BUILD_TYPE@\n#cmakedefine Q4\n",
            "a/x.cpp": '#include "c.h"\n',
        })
        self.assertEqual(lib.node_by_output(graph, "$(B)/a/c.h")["cmds"][0]["cmd_args"][4:], [
            "BUILD_TYPE=DEBUG", "Q1=quoted", "Q2=plain", "Q3=bare", "Q4=dflt",
        ])

    def test_include_dirs_from_variables(self):
        files = {
            "a/ya.make": module("LIBRARY", (
                "SET(P ${ARCADIA_ROOT}/l)\nPEERDIR(ADDINCL ${P})\n"
                "ADDINCL(GLOBAL FOR asm a/gasm)\nSRCS(x.cpp s.asm)"
            )),
            "a/x.cpp": "", "a/s.asm": "", "a/gasm/x.inc": "",
            "l/ya.make": module("LIBRARY", ""),
        }
        lib.tool_program(files, "contrib/tools/yasm", "yasm")
        graph, _ = self.make(files, "a", "-k")
        self.assertEqual(includes(graph, "$(B)/a/x.cpp.o"), ["-I$(B)", "-I$(S)", "-I$(S)/l"])
        asm = lib.node_by_output(graph, "$(B)/a/s.o")["cmds"][0]["cmd_args"]
        self.assertEqual([asm[i + 1] for i, arg in enumerate(asm) if arg == "-I"], ["$(B)", "$(S)", "$(S)/l", "$(S)/a/gasm"])

    def test_clang_bitcode_root_bound_to_a_path(self):
        code, _, stderr = ay_make({
            "a/ya.make": module("LIBRARY", (
                "SET(CLANG_BC_ROOT ${ARCADIA_ROOT}/myclang)\nSET(LLVM_LLC_TOOL tools/llc)\nLLVM_BC(x.cpp NAME n)"
            )),
            "a/x.cpp": "",
        }, "a")
        self.assertEqual(code, 1)
        self.assertEqual(stderr, 'resources: "$(S)/myclang" references resource global not in the PEERDIR closure')

    def test_modules_that_are_their_own_implicit_peers(self):
        files = {
            "library/cpp/resource/ya.make": module("LIBRARY", "RESOURCE(r.txt key)"),
            "library/cpp/resource/r.txt": "",
        }
        lib.tool_program(files, "tools/rescompiler", "rescompiler")
        lib.tool_program(files, "tools/rescompressor", "rescompressor")
        graph, stderr = self.make(files, "library/cpp/resource")
        self.assertEqual(stderr, "")
        lib.node_by_output_prefix(graph, "$(B)/library/cpp/resource/objcopy_")

        for path, expected in (
            ("contrib/libs/python", ["-I$(B)", "-I$(S)"]),
            ("b", ["-I$(B)", "-I$(S)", "-I$(S)/contrib/libs/python/Include", "-DUSE_PYTHON3"]),
        ):
            with self.subTest(path=path):
                files = {
                    f"{path}/ya.make": module("PY3_LIBRARY", "SRCS(x.cpp)"),
                    f"{path}/x.cpp": "",
                }
                if path != "contrib/libs/python":
                    files["contrib/libs/python/ya.make"] = module("LIBRARY", "")
                graph, _ = self.make(files, path)
                args = lib.node_by_output(graph, f"$(B)/{path}/x.cpp.o")["cmds"][0]["cmd_args"]
                self.assertEqual([arg for arg in args if arg.startswith("-I") or arg == "-DUSE_PYTHON3"], expected)

    def test_host_tool_join_sources_scan_target_arch_headers(self):
        graph, _ = self.make({
            "tools/gen/ya.make": module("PROGRAM", "PEERDIR(musl)\nJOIN_SRCS(all.cpp a.cpp)"),
            "tools/gen/a.cpp": "#include <bits/alltypes.h>\nint main(){return 0;}\n",
            "musl/ya.make": module("LIBRARY", "ADDINCL(GLOBAL contrib/libs/musl/arch/x86_64 GLOBAL musl/other)"),
            "musl/other/o.h": "",
            "contrib/libs/musl/arch/x86_64/bits/alltypes.h": "// x86_64\n",
            "contrib/libs/musl/arch/aarch64/bits/alltypes.h": "// aarch64\n",
            "a/ya.make": module("LIBRARY", "RUN_PROGRAM(tools/gen OUT x.cpp)"),
        })
        self.assertEqual(
            lib.node_by_output(graph, "$(B)/tools/gen/all.cpp")["inputs"],
            ["$(S)/tools/gen/a.cpp", "$(S)/contrib/libs/musl/arch/aarch64/bits/alltypes.h"],
        )
        self.assertIn(
            "$(S)/contrib/libs/musl/arch/x86_64/bits/alltypes.h",
            lib.node_by_output(graph, "$(B)/tools/gen/all.cpp.pic.o")["inputs"],
        )


if __name__ == "__main__":
    unittest.main(verbosity=2)
