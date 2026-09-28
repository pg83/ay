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


def kinds(graph, prefix="$(B)/a/"):
    return sorted(
        (node["kv"]["p"], node["outputs"][0])
        for node in graph["graph"]
        if node["outputs"][0].startswith(prefix)
    )


class YaMakeMiscMacrosTest(unittest.TestCase):
    def make(self, files, target="a", *args):
        code, graph, stderr = ay_make(files, target, *args)
        self.assertEqual(code, 0, stderr)
        return graph

    def llvm_tree(self, body):
        files = {
            "a/ya.make": module("LIBRARY", "PEERDIR(res)\n" + body),
            "a/x.cpp": "int x;\n",
            "a/y.c": "int y;\n",
            "res/ya.make": (
                "RESOURCES_LIBRARY()\n"
                "DECLARE_EXTERNAL_RESOURCE(CLANG16 sbr:16)\n"
                "DECLARE_EXTERNAL_RESOURCE(CLANG18 sbr:18)\n"
                "DECLARE_EXTERNAL_RESOURCE(CLANG20 sbr:20)\n"
                "END()\n"
            ),
            "library/cpp/resource/ya.make": module("LIBRARY", ""),
        }
        lib.tool_program(files, "tools/rescompiler", "rescompiler")
        lib.tool_program(files, "tools/rescompressor", "rescompressor")
        return files

    def test_llvm_bitcode(self):
        graph = self.make(self.llvm_tree(
            "USE_LLVM_BC16()\nLLVM_BC(x.cpp NAME b16)\n"
            "USE_LLVM_BC20()\nLLVM_BC(x.cpp y.c NAME b20 SUFFIX .sfx SYMBOLS s1 s2 NO_COMPILE)\n"
            "USE_LLVM_BC18()\nLLVM_BC(x.cpp NAME b18 GENERATE_MACHINE_CODE)"
        ))
        self.assertEqual(
            [output for kind, output in kinds(graph) if kind in ("LD", "OP")],
            ["$(B)/a/b16_merged.bc", "$(B)/a/b20_merged.sfx.bc", "$(B)/a/b16_optimized.bc", "$(B)/a/b20_optimized.sfx.bc"],
        )
        compile_16 = lib.node_by_output(graph, "$(B)/a/x.cpp.bc")["cmds"][0]["cmd_args"]
        self.assertEqual(compile_16[3], "$(B)/resources/CLANG16/bin/clang++")
        optimize_20 = lib.node_by_output(graph, "$(B)/a/b20_optimized.sfx.bc")["cmds"][0]["cmd_args"]
        self.assertIn("-internalize-public-api-list=s1#s2", optimize_20)
        self.assertEqual(optimize_20[2], "$(B)/resources/CLANG20/bin/opt")

    def test_check_config_and_cython_builds(self):
        graph = self.make({
            "a/ya.make": module("LIBRARY", "CHECK_CONFIG_H(conf.h)\nBUILDWITH_CYTHON_CPP(c.pyx --opt)\nBUILDWITH_CYTHON_C(d.pyx)"),
            "a/conf.h": "\n", "a/c.pyx": "\n", "a/d.pyx": "\n",
        }, "a", "-k")
        self.assertEqual([item for item in kinds(graph) if item[0] in ("CH", "CY")], [
            ("CH", "$(B)/a/conf.config.cpp"),
            ("CY", "$(B)/a/c.pyx.cpp"),
            ("CY", "$(B)/a/d.pyx.c"),
        ])
        self.assertIn("--opt", lib.node_by_output(graph, "$(B)/a/c.pyx.cpp")["cmds"][0]["cmd_args"])

    def test_peer_link_libraries_and_rpath(self):
        graph = self.make({
            "p/ya.make": module("PROGRAM", "PEERDIR(l)\nSRCS(m.cpp)"),
            "p/m.cpp": "int main(){return 0;}\n",
            "l/ya.make": module("LIBRARY", (
                "EXTRALIBS(z -lpthread)\nEXTRALIBS()\n"
                "SET_APPEND(RPATH_GLOBAL '-Wl,-rpath,${\"$\"}ORIGIN/lib')\n"
                "SRCS(l.cpp)"
            )),
            "l/l.cpp": "int l;\n",
        }, "p")
        link = lib.node_by_output(graph, "$(B)/p/p")["cmds"][-2]["cmd_args"]
        trailer = link[link.index("-Wl,--gdb-index") + 1:link.index("-nodefaultlibs")]
        self.assertEqual(trailer, ["-Wl,-rpath,$ORIGIN/lib", "-lz", "-lpthread"])

    def test_bundle_suffix_and_name(self):
        files = {
            "a/ya.make": module("LIBRARY", "BUNDLE(dep SUFFIX .sfx dep NAME named.bin)\nRESOURCE(dep.sfx k1 named.bin k2)"),
            "dep/ya.make": module("LIBRARY", "SRCS(d.cpp)"),
            "dep/d.cpp": "int d;\n",
            "library/cpp/resource/ya.make": module("LIBRARY", ""),
        }
        lib.tool_program(files, "tools/rescompiler", "rescompiler")
        lib.tool_program(files, "tools/rescompressor", "rescompressor")
        graph = self.make(files)
        self.assertEqual([output for kind, output in kinds(graph) if kind == "BN"], [
            "$(B)/a/dep.sfx", "$(B)/a/named.bin",
        ])


if __name__ == "__main__":
    unittest.main(verbosity=2)
