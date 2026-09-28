import json
import os
import subprocess
import tempfile
import unittest
from pathlib import Path

import lib


GZ_RULE = "debug_info_flags.append('-gz=zstd')"


def library(sources, extra=""):
    return (
        "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
        f"{extra}SRCS({' '.join(sources)})\nEND()\n"
    )


RAGEL = {
    "lexer/ya.make": library(["lexer.rl6", "a.cpp", "b.c"]),
    "lexer/lexer.rl6": "%%{ machine m; }%%\n",
    "lexer/a.cpp": "int a(){return 0;}\n",
    "lexer/b.c": "int b(){return 0;}\n",
    "contrib/tools/ragel6/ya.make": (
        "PROGRAM(ragel6)\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(main.cpp)\nEND()\n"
    ),
    "contrib/tools/ragel6/main.cpp": "int main(){return 0;}\n",
}


class Tree:
    def __init__(self, test, files, ya_conf='[flags]\nOPENSOURCE = "yes"\n\n[host_platform_flags]\nOPENSOURCE = "yes"\n'):
        tmp = tempfile.TemporaryDirectory(prefix="ay-platform-test-")
        test.addCleanup(tmp.cleanup)
        self.src = Path(tmp.name).resolve()
        for relative, content in {".arcadia.root": "", "ya.conf": ya_conf, **files}.items():
            path = self.src / relative
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(content)

    def graph(self, *args, **env_extra):
        env = {k: v for k, v in os.environ.items() if k not in lib.TOOLCHAIN_ENV_VARS}
        env.update(env_extra)
        result = subprocess.run(
            [str(lib.AY), "make", "-j0", "-G", "--source-root", str(self.src),
             "--host-platform", "default-linux-x86_64",
             "--target-platform", "default-linux-aarch64", *args],
            env=env,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
            timeout=60,
            check=False,
        )
        if result.returncode != 0:
            raise AssertionError(f"exit {result.returncode}\n{result.stderr}")
        return json.loads(result.stdout), result.stderr


def args_of(graph, output):
    return lib.node_by_output(graph, output)["cmds"][0]["cmd_args"]


def picked(args, *needles):
    return [a for a in args if any(n in a for n in needles)]


class CompilerFlagSourcesTest(unittest.TestCase):
    def test_config_internal_config_and_environment_flags_in_order(self):
        tree = Tree(self, {
            **RAGEL,
            "build/internal/ya.conf": (
                '[flags]\nCFLAGS = "-DT_INTERNAL"\nCXXFLAGS = " -DTXX_INTERNAL "\n'
                '[host_platform_flags]\nCFLAGS = "-DH_INTERNAL"\n'
            ),
        }, ya_conf=(
            '[flags]\nOPENSOURCE = "yes"\nCFLAGS = "-DT_CONF"\n\n'
            '[host_platform_flags]\nOPENSOURCE = "yes"\nCXXFLAGS = "-DHXX_CONF"\n'
        ))
        graph, _ = tree.graph("lexer", CFLAGS="-DT_ENV", CXXFLAGS="-DTXX_ENV")
        self.assertEqual(
            picked(args_of(graph, "$(B)/lexer/a.cpp.o"), "-DT", "-DH"),
            ["-DT_CONF", "-DT_INTERNAL", "-DT_ENV", "-DTXX_INTERNAL", "-DTXX_ENV"],
        )
        self.assertEqual(
            picked(args_of(graph, "$(B)/lexer/b.c.o"), "-DT", "-DH"),
            ["-DT_CONF", "-DT_INTERNAL", "-DT_ENV"],
        )
        self.assertEqual(
            picked(args_of(graph, "$(B)/contrib/tools/ragel6/main.cpp.pic.o"), "-DT", "-DH"),
            ["-DH_INTERNAL", "-DHXX_CONF"],
        )


class PlatformVariantsTest(unittest.TestCase):
    def test_musl_without_opensource_uses_no_sysroot(self):
        tree = Tree(self, RAGEL, ya_conf="[flags]\n")
        graph, _ = tree.graph("lexer")
        self.assertEqual(
            picked(args_of(graph, "$(B)/lexer/a.cpp.o"), "--sysroot", "-B$(B)"),
            ["--sysroot=$(B)/resources/OS_SDK_ROOT", "-B$(B)/resources/OS_SDK_ROOT/usr/bin"],
        )
        graph, _ = tree.graph("--musl", "lexer")
        self.assertEqual(
            picked(args_of(graph, "$(B)/lexer/a.cpp.o"), "--sysroot", "-B$(B)"),
            ["--sysroot=/nowhere", "-B$(B)/resources/OS_SDK_ROOT/usr/bin"],
        )

    def test_clang_version_selects_resource_paths(self):
        # Compile nodes carry the host platform's tool environment.
        tree = Tree(self, RAGEL)
        library_path = "DYLD_LIBRARY_PATH"
        for flags, prefix in (
            ((), "$(B)/resources/CLANG20/lib:"),
            (("--host-platform-flag", "CLANG_VER="), "$(B)/resources/CLANG20/lib:"),
            (("--host-platform-flag", "CLANG_VER=18"), "$(B)/resources/CLANG18/lib:"),
        ):
            graph, _ = tree.graph(*flags, "lexer")
            env = lib.node_by_output(graph, "$(B)/lexer/b.c.o")["cmds"][0]["env"]
            self.assertTrue(env[library_path].startswith(prefix), (flags, env))

    def test_ymake_conf_enables_compressed_debug_sections(self):
        plain_tree = Tree(self, {**RAGEL, "build/ymake_conf.py": "debug_info_flags = []\n"})
        graph, _ = plain_tree.graph("lexer")
        self.assertNotIn("-gz=zstd", args_of(graph, "$(B)/lexer/a.cpp.o"))
        tree = Tree(self, {**RAGEL, "build/ymake_conf.py": f"if x:\n    {GZ_RULE}\n"})
        graph, _ = tree.graph("lexer")
        self.assertIn("-gz=zstd", args_of(graph, "$(B)/lexer/a.cpp.o"))
        graph, _ = tree.graph("-r", "lexer")
        self.assertNotIn("-gz=zstd", args_of(graph, "$(B)/lexer/a.cpp.o"))

    def test_ownership_debugging_reports_on_exit(self):
        tree = Tree(self, RAGEL)
        graph, stderr = tree.graph("lexer", AY_DEBUG_OWNERSHIP="1")
        self.assertIn("$(B)/lexer/a.cpp.o", [o for n in graph["graph"] for o in n["outputs"]])
        self.assertRegex(stderr, r"(^|\n)ownership: \d+ violating \(field, site\) pairs\n")


class ScriptDependenciesTest(unittest.TestCase):
    def test_import_forms_and_transitive_closure(self):
        tree = Tree(self, {
            "build/platform/res/ya.make": (
                "RESOURCES_LIBRARY()\nDECLARE_EXTERNAL_RESOURCE(RES sbr:1)\nEND()\n"
            ),
            "build/scripts/fetch_from_sandbox.py": (
                "import os, alpha as a\n"
                "from beta import thing\n"
                "from .gamma import other\n"
                "from . import delta\n"
                "import epsilon.sub\n"
                "import fetch_from_sandbox\n"
                "x = 1  # import zeta\n"
            ),
            "build/scripts/fetch_from_mds.py": "",
            "build/scripts/alpha.py": "import beta\n",
            "build/scripts/beta.py": "import alpha\nfrom nested import n\n",
            "build/scripts/gamma.py": "",
            "build/scripts/delta.py": "",
            "build/scripts/epsilon.py": "",
            "build/scripts/zeta.py": "",
            "build/scripts/lib/nested.py": "",
        })
        graph, _ = tree.graph("--sandboxing", "build/platform/res")
        fetch = lib.node_by_output(graph, "$(B)/resources/RES")
        self.assertEqual(fetch["inputs"], [
            "$(S)/build/mapping.conf.json",
            "$(S)/build/scripts/fetch_from_sandbox.py",
            "$(S)/build/scripts/alpha.py",
            "$(S)/build/scripts/beta.py",
            "$(S)/build/scripts/epsilon.py",
            "$(S)/build/scripts/gamma.py",
            "$(S)/build/scripts/lib/nested.py",
            "$(S)/build/scripts/fetch_from_mds.py",
        ])


ARM = {
    "arm/ya.make": (
        "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(a.cpp)\n"
        "IF (ARCH_AARCH64)\n    SRCS(aarch64.cpp)\nENDIF()\n"
        "IF (ARCH_ARM64)\n    SRCS(arm64.cpp)\nENDIF()\nEND()\n"
    ),
    "arm/a.cpp": "int a(){return 0;}\n",
    "arm/aarch64.cpp": "int b(){return 0;}\n",
    "arm/arm64.cpp": "int c(){return 0;}\n",
}


class Arm64TargetTest(unittest.TestCase):
    # macOS calls 64-bit ARM "arm64". Upstream counts it as armv8: ARCH_ARM64
    # and ARCH_AARCH64 are set, so -mno-outline-atomics applies, while
    # -march=armv8-a is added for Linux only.
    def test_darwin_arm64_target_compiles_as_aarch64_family(self):
        tree = Tree(self, ARM)
        for target, march in (
            ("default-darwin-arm64", []),
            ("default-linux-aarch64", ["-march=armv8-a"]),
        ):
            with self.subTest(target=target):
                graph, _ = tree.graph("--target-platform", target, "arm")
                node = lib.node_by_output(graph, "$(B)/arm/a.cpp.o")
                self.assertEqual(node["platform"], target)
                args = node["cmds"][0]["cmd_args"]
                self.assertIn("-mno-outline-atomics", args)
                self.assertEqual(picked(args, "-march"), march)
                for source in ("aarch64", "arm64"):
                    lib.node_by_output(graph, f"$(B)/arm/{source}.cpp.o")


if __name__ == "__main__":
    unittest.main(verbosity=2)
