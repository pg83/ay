import json
import os
import re
import subprocess
import tempfile
import unittest
from pathlib import Path

import lib


ANSI = re.compile(r"\x1b\[[0-9;]*[A-Za-z]")


def plain(text):
    return ANSI.sub("", text)


def library(sources, extra=""):
    return (
        "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
        f"{extra}SRCS({' '.join(sources)})\nEND()\n"
    )


def program(name, sources, extra=""):
    return (
        f"PROGRAM({name})\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
        f"{extra}SRCS({' '.join(sources)})\nEND()\n"
    )


class Tree:
    def __init__(self, test, files, ya_conf='[flags]\nOPENSOURCE = "yes"\n'):
        tmp = tempfile.TemporaryDirectory(prefix="ay-flags-test-")
        test.addCleanup(tmp.cleanup)
        self.root = Path(tmp.name).resolve()
        self.src = self.root / "src"
        self.home = self.root / "home"
        self.home.mkdir()
        for relative, content in {".arcadia.root": "", "ya.conf": ya_conf, **files}.items():
            path = self.src / relative
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(content)

    def env(self, **extra):
        env = {
            key: value
            for key, value in os.environ.items()
            if key not in lib.TOOLCHAIN_ENV_VARS
        }
        env["HOME"] = str(self.home)
        env.update(extra)
        return env

    def ay(self, *args, cwd=None, env=None):
        return subprocess.run(
            [str(lib.AY), *map(str, args)],
            cwd=cwd,
            env=env if env is not None else self.env(),
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
            timeout=30,
            check=False,
        )

    def graph(self, *args, env=None):
        result = self.ay(
            "make", "-j0", "-G", "--source-root", self.src,
            "--host-platform", "default-linux-x86_64", *args, env=env,
        )
        if result.returncode != 0:
            raise AssertionError(f"exit {result.returncode}\n{result.stderr}")
        return json.loads(result.stdout), result.stderr


def cc_args(graph, output):
    return lib.node_by_output(graph, output)["cmds"][0]["cmd_args"]


def compiled_sources(graph):
    return sorted(
        Path(node["outputs"][0]).name
        for node in graph["graph"]
        if node["kv"].get("p") == "CC"
    )


SWITCHED = {
    "sw/ya.make": (
        "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
        "IF (MY_SWITCH)\n    SRCS(on.cpp)\nELSE()\n    SRCS(off.cpp)\nENDIF()\n"
        "IF (TESTS_REQUESTED)\n    SRCS(tested.cpp)\nENDIF()\n"
        "END()\n"
    ),
    "sw/on.cpp": "int on(){return 1;}\n",
    "sw/off.cpp": "int off(){return 0;}\n",
    "sw/tested.cpp": "int tested(){return 0;}\n",
}


RAGEL = {
    "lexer/ya.make": library(["lexer.rl6"]),
    "lexer/lexer.rl6": "%%{ machine m; }%%\n",
    "contrib/tools/ragel6/ya.make": (
        "PROGRAM(ragel6)\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
        "IF (HOST_SWITCH)\n    SRCS(host_on.cpp)\nELSE()\n    SRCS(host_off.cpp)\nENDIF()\n"
        "END()\n"
    ),
    "contrib/tools/ragel6/host_on.cpp": "int main(){return 1;}\n",
    "contrib/tools/ragel6/host_off.cpp": "int main(){return 0;}\n",
}


class MakeUsageTest(unittest.TestCase):
    def test_help_is_colorized_and_exits_zero(self):
        tree = Tree(self, {})
        for flag in ("-h", "--help"):
            result = tree.ay("make", flag)
            self.assertEqual(result.returncode, 0)
            self.assertTrue(plain(result.stdout).startswith("usage: ay make [flags] [targets...]\n"))
            self.assertIn("\x1b[", result.stdout)
            self.assertIn("  -j, --jobs <N>  ", plain(result.stdout))
            self.assertIn("\nconfiguration flags:\n", plain(result.stdout))

    def test_invalid_arguments(self):
        tree = Tree(self, {})
        for args, message in (
            (("--bogus",), "getopt: unrecognized option"),
            (("--xbuild", ""), "getopt: option requires an argument"),
            (("-j", "many", "x"), 'strconv.Atoi: parsing "many": invalid syntax'),
            (("--target-platform", "linux-x86_64", "x"), 'does not start with "default-"'),
            (("--target-platform", "default-linux", "x"), "lacks the <os>-<isa> separator"),
        ):
            result = tree.ay("make", "--source-root", tree.src, *args)
            self.assertEqual(result.returncode, 1, args)
            self.assertIn(message, plain(result.stderr), args)

    def test_no_targets_outside_source_root(self):
        tree = Tree(self, {})
        result = tree.ay("make", "-j0", "--source-root", tree.src, cwd=tree.root)
        self.assertEqual(result.returncode, 1)
        self.assertIn(
            "make: no targets supplied and current working directory is outside the source root",
            result.stderr,
        )

    def test_source_root_found_from_working_directory(self):
        tree = Tree(self, {"sw/ya.make": library(["off.cpp"]), "sw/off.cpp": "int f();\n"})
        result = tree.ay(
            "make", "-j0", "-G", "--host-platform", "default-linux-x86_64",
            cwd=tree.src / "sw",
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        graph = json.loads(result.stdout)
        self.assertEqual(compiled_sources(graph), ["off.cpp.o"])

    def test_without_ya_conf_the_working_directory_is_the_root(self):
        tree = Tree(self, {})
        lonely = tree.root / "lonely"
        lonely.mkdir()
        result = tree.ay("make", "-j0", cwd=lonely)
        self.assertEqual(result.returncode, 1)
        # Linux opens files relative to the root directory, other systems by
        # full path, so the message names ya.conf with or without the root.
        self.assertRegex(plain(result.stderr), r"open (\S*/)?ya\.conf: no such file or directory")

    def test_generation_without_dump(self):
        tree = Tree(self, SWITCHED)
        result = tree.ay(
            "make", "-j0", "--source-root", tree.src,
            "--host-platform", "default-linux-x86_64", "sw",
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout, "")


class MakeConfigurationFlagsTest(unittest.TestCase):
    def test_defines_switch_ya_make_conditions(self):
        tree = Tree(self, SWITCHED)
        graph, _ = tree.graph("sw")
        self.assertEqual(compiled_sources(graph), ["off.cpp.o"])
        for define in (("-D", "MY_SWITCH"), ("--define", "MY_SWITCH=yes"), ("-DMY_SWITCH=1",)):
            graph, _ = tree.graph(*define, "sw")
            self.assertEqual(compiled_sources(graph), ["on.cpp.o"], define)
        graph, _ = tree.graph("-t", "sw")
        self.assertEqual(compiled_sources(graph), ["off.cpp.o", "tested.cpp.o"])

    def test_build_type_flags(self):
        tree = Tree(self, SWITCHED)
        out = "$(B)/sw/off.cpp.o"
        debug, _ = tree.graph("sw")
        self.assertIn("-UNDEBUG", cc_args(debug, out))
        for flags in (("-r",), ("--release",), ("--xbuild", "profile"), ("-r", "-d", "-r")):
            graph, _ = tree.graph(*flags, "sw")
            self.assertIn("-DNDEBUG", cc_args(graph, out), flags)
            self.assertNotIn("-UNDEBUG", cc_args(graph, out), flags)
        graph, _ = tree.graph("-r", "--debug", "sw")
        self.assertIn("-UNDEBUG", cc_args(graph, out))

    def test_host_build_type_falls_back_to_build_type_flag(self):
        tree = Tree(self, RAGEL)
        tool = "$(B)/contrib/tools/ragel6/host_off.cpp.pic.o"
        for flags, expected in (
            ((), "-DNDEBUG"),
            (("--host-platform-flag", "GG_BUILD_TYPE="), "-UNDEBUG"),
            (("--host-platform-flag", "GG_BUILD_TYPE=", "--host-platform-flag", "BUILD_TYPE=Release"), "-DNDEBUG"),
            (("--host-platform-flag", "GG_BUILD_TYPE=Debug"), "-UNDEBUG"),
        ):
            graph, _ = tree.graph("--target-platform", "default-linux-aarch64", *flags, "lexer")
            self.assertIn(expected, cc_args(graph, tool), flags)

    def test_musl_links_without_default_libs(self):
        tree = Tree(self, {"p/ya.make": program("p", ["m.cpp"]), "p/m.cpp": "int main(){}\n"})
        graph, _ = tree.graph("--musl", "p")
        link = lib.node_by_output(graph, "$(B)/p/p")["cmds"][2]["cmd_args"]
        self.assertIn("-nostdlib", link)
        self.assertNotIn("-lc", link)

    def test_host_platform_flags_reach_host_tools(self):
        tree = Tree(self, RAGEL)
        graph, _ = tree.graph("--target-platform", "default-linux-aarch64", "lexer")
        self.assertIn("host_off.cpp.pic.o", compiled_sources(graph))
        graph, _ = tree.graph(
            "--target-platform", "default-linux-aarch64",
            "--host-platform-flag", "HOST_SWITCH", "lexer",
        )
        self.assertIn("host_on.cpp.pic.o", compiled_sources(graph))
        self.assertNotIn("host_off.cpp.pic.o", compiled_sources(graph))

    def test_layout_flags_are_accepted_in_graph_mode(self):
        tree = Tree(self, SWITCHED)
        graph, _ = tree.graph(
            "-o", tree.root / "out", "--output", tree.root / "out",
            "-B", tree.root / "b", "--build-dir", tree.root / "b",
            "-I", tree.root / "i", "--install", tree.root / "i",
            "--jobs", "0", "--keep-going", "--dump-graph", "--stat", "-T", "--ninja",
            "--clear", "--cmd-prefix", "x=y", "sw",
        )
        self.assertEqual(compiled_sources(graph), ["off.cpp.o"])
        self.assertFalse((tree.root / "b").exists())


class MakeWarningsTest(unittest.TestCase):
    UNKNOWN = {
        "w/ya.make": library(["a.cpp"], "NOT_A_REAL_MACRO(x)\nNOT_A_REAL_MACRO(y)\n"),
        "w/a.cpp": "int a(){return 0;}\n",
    }

    def test_unknown_macro_is_fatal_without_keep_going(self):
        tree = Tree(self, self.UNKNOWN)
        result = tree.ay(
            "make", "-j0", "-G", "--source-root", tree.src,
            "--host-platform", "default-linux-x86_64", "w",
        )
        self.assertEqual(result.returncode, 1)
        self.assertIn('unknown-macro: w: macro "NOT_A_REAL_MACRO" is not in the modelled set (line 5); skipped', plain(result.stderr))

    def test_keep_going_reports_fatal_warnings(self):
        tree = Tree(self, self.UNKNOWN)
        graph, stderr = tree.graph("-k", "w")
        self.assertEqual(compiled_sources(graph), ["a.cpp.o"])
        lines = [line for line in stderr.splitlines() if "NOT_A_REAL_MACRO" in line]
        self.assertEqual([plain(line) for line in lines], [
            'unknown-macro: w: macro "NOT_A_REAL_MACRO" is not in the modelled set (line 5); skipped',
            'unknown-macro: w: macro "NOT_A_REAL_MACRO" is not in the modelled set (line 6); skipped',
        ])
        self.assertTrue(lines[0].startswith("\x1b[33m"))

    def test_missing_addincl_is_reported_once_without_verbose(self):
        tree = Tree(self, {
            "w/ya.make": library(["a.cpp"], "ADDINCL(no/such/dir)\nADDINCL(no/such/dir)\n"),
            "w/a.cpp": "int a(){return 0;}\n",
        })
        _, stderr = tree.graph("w")
        self.assertEqual(
            plain(stderr),
            "missing-addincl: w: ADDINCL to non existent source directory no/such/dir\n",
        )

    def test_diagnostics_need_verbose(self):
        tree = Tree(self, {
            "w/ya.make": library(["a.cpp"]),
            "w/a.cpp": "int a(){return 0;}\n",
            "build/sysincl/macro.yml": "- odd_key: 1\n  includes:\n    - vector\n",
        })
        _, quiet = tree.graph("w")
        self.assertNotIn("sysincl", quiet)
        message = 'sysincl: macro.yml:1: unrecognised record key "odd_key"'
        _, verbose = tree.graph("--verbose", "w")
        self.assertIn(message, plain(verbose))
        result = tree.ay(
            "--verbose", "make", "-j0", "-G", "--source-root", tree.src,
            "--host-platform", "default-linux-x86_64", "w",
        )
        self.assertIn(message, plain(result.stderr))

    def test_dump_ignored_macros(self):
        tree = Tree(self, {
            "w/ya.make": library(["a.cpp"], "SUBSCRIBER(g:team)\n"),
            "w/a.cpp": "int a(){return 0;}\n",
        })
        _, stderr = tree.graph("--dump-ignored-macros", "w")
        self.assertIn("=== ya.make macros gen acknowledges but emits nothing for ===", stderr)
        self.assertRegex(stderr, r"\n\s+SUBSCRIBER\b")


if __name__ == "__main__":
    unittest.main(verbosity=2)
