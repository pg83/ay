import json
import os
import platform
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

import lib


ESC = "\x1b"
RED = ESC + "[31m"
RESET = ESC + "[0m"
USAGE_HEAD = ESC + "[92musage:" + RESET + " ay [global-flags] "


def run_ay(*args, env=None, cwd=None, timeout=30):
    return subprocess.run(
        [str(lib.AY), *map(str, args)],
        env=env,
        cwd=cwd,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
        timeout=timeout,
        check=False,
    )


def fatal(message):
    return RED + message + RESET + "\n"


def toolchain_free_env(**extra):
    env = {
        key: value
        for key, value in os.environ.items()
        if key not in lib.TOOLCHAIN_ENV_VARS
    }
    env.update(extra)
    return env


LIBRARY_YA_MAKE = (
    "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(a.c b.c)\nEND()\n"
)


class DevtoolsCliTest(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory(prefix="ay-cli-test-")
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)

    def source_root(self, ya_conf):
        root = self.root / "src"
        (root / "lib").mkdir(parents=True)
        (root / ".arcadia.root").touch()
        (root / "ya.conf").write_text(ya_conf)
        (root / "lib/ya.make").write_text(LIBRARY_YA_MAKE)
        (root / "lib/a.c").write_text("int f(void){return 0;}\n")
        (root / "lib/b.c").write_text("int g(void){return 0;}\n")
        return root

    def make(self, root, *args, env=None):
        return run_ay(
            "make", "-j0", "-G", "--source-root", root, *args, "lib",
            env=env or toolchain_free_env(),
        )

    def test_top_level_usage_collapses_dev_commands(self):
        result = run_ay()
        self.assertEqual(result.returncode, 2)
        self.assertEqual(result.stdout, "")
        text = result.stderr
        self.assertTrue(text.startswith(USAGE_HEAD + "<subcommand> [args]\n"))
        self.assertIn(ESC + "[91mmake:" + RESET, text)
        self.assertIn(
            ESC + "[91mdev:" + RESET
            + "\n    🛠️ Developer tooling (dump, perf, refac, probe). Pass --verbose to list.",
            text,
        )
        self.assertNotIn("dev dump sort", text)
        self.assertNotIn("fetch", text)
        self.assertNotIn("--probe", text)

    def test_verbose_usage_lists_hidden_and_dev_commands(self):
        for flag in ("-v", "--verbose"):
            result = run_ay(flag)
            self.assertEqual(result.returncode, 2)
            text = result.stderr
            self.assertIn(ESC + "[93m--probe=map|callsite|str" + RESET, text)
            for name in (
                "fetch:", "fetch base64:", "fetch sandbox:", "make:",
                "dev cas:", "dev maxrss:", "dev dump normalize:",
                "dev perf buckethash:", "dev refac case:",
                "dev probe callsite:",
            ):
                self.assertIn(ESC + "[91m" + name + RESET, text)
            self.assertNotIn("Pass --verbose to list", text)

    def test_group_prefix_prints_group_usage(self):
        result = run_ay("dev", "dump")
        self.assertEqual(result.returncode, 2)
        text = result.stderr
        self.assertTrue(text.startswith(USAGE_HEAD + "dev dump <subcommand> [args]\n"))
        for name in ("normalize", "sort", "diff", "grep"):
            self.assertIn(ESC + "[91mdev dump " + name + ":" + RESET, text)
        self.assertNotIn("dev perf", text)
        self.assertNotIn("make:", text)

    def test_unknown_subcommand(self):
        for argv in (["bogus"], ["dev", "nothing"], ["-h"], [""]):
            result = run_ay(*argv)
            self.assertEqual(result.returncode, 2)
            self.assertTrue(result.stderr.startswith(
                "unknown subcommand: " + " ".join(argv) + "\n" + USAGE_HEAD
            ))

    def test_unknown_global_flags_are_fatal(self):
        cases = {
            ("--bogus",): 'unknown global flag "--bogus"',
            ("--probe=str", "make"): 'unknown --probe="str" (want map|callsite)',
        }
        for argv, message in cases.items():
            result = run_ay(*argv)
            self.assertEqual(result.returncode, 1)
            self.assertEqual(result.stderr, fatal(message))

    def test_probe_flags_without_instrumentation_report_nothing(self):
        source = self.root / "in.txt"
        source.write_text("b\na\n")
        for flag in ("--probe=map", "--probe=callsite", "-probe=map"):
            output = self.root / "out.txt"
            result = run_ay(flag, "dev", "dump", "sort", "--in", source, "--out", output)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(result.stderr, "")
            self.assertEqual(output.read_text(), "a\nb\n")

    def test_callsite_out_env_appends_nothing_without_instrumentation(self):
        source = self.root / "in.txt"
        source.write_text("b\na\n")
        sites = self.root / "sites.txt"
        sites.write_text("pre-existing\n")
        env = dict(os.environ, CALLSITE_OUT=str(sites))
        result = run_ay(
            "dev", "dump", "sort", "--in", source, "--out", self.root / "o1",
            env=env,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(sites.read_text(), "pre-existing\n")
        env = dict(os.environ, CALLSITE_OUT=str(self.root / "missing/dir/sites"))
        result = run_ay(
            "dev", "dump", "sort", "--in", source, "--out", self.root / "o2",
            env=env,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertFalse((self.root / "missing").exists())

    def test_profiling_environment_writes_profiles(self):
        source = self.root / "in.txt"
        source.write_text("".join(f"{i:06d}\n" for i in range(2000, 0, -1)))
        output = self.root / "out.txt"
        cpu = self.root / "cpu.pprof"
        mem = self.root / "mem.pprof"
        env = dict(
            os.environ,
            YATOOL_CPUPROFILE=str(cpu),
            YATOOL_CPUHZ="500",
            YATOOL_MEMPROFILE=str(mem),
            YATOOL_MEMPROFRATE="1",
        )
        result = run_ay(
            "dev", "dump", "sort", "--in", source, "--out", output,
            "--chunk-bytes", "64",
            env=env,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(output.read_text().splitlines()[:2], ["000001", "000002"])
        for profile in (cpu, mem):
            self.assertEqual(profile.read_bytes()[:2], b"\x1f\x8b", profile)

    def test_make_without_platforms_uses_host(self):
        root = self.source_root('[flags]\nOPENSOURCE = "yes"\n')
        result = self.make(root)
        self.assertEqual(result.returncode, 0, result.stderr)
        graph = json.loads(result.stdout)
        compile_node = lib.node_by_output(graph, "$(B)/lib/a.c.o")
        # ay names the host after GOOS and its ISA: x86_64, aarch64 on Linux,
        # arm64 on macOS, which are also what Python reports for the host.
        self.assertEqual(compile_node["platform"], f"default-{sys.platform}-{platform.machine()}")

    def test_ya_conf_scalar_types_reach_flags(self):
        root = self.source_root(
            "host_platform_flags = 1\n"
            "[flags]\n"
            'OPENSOURCE = "yes"\n'
            'CFLAGS = "-DFROM_CONF"\n'
            "BOOL_FLAG = true\n"
            "INT_FLAG = 42\n"
            "FLOAT_FLAG = 1.5\n"
            "LIST_FLAG = [1, 2]\n"
        )
        result = self.make(
            root,
            "--target-platform", "default-linux-aarch64",
            "--host-platform", "default-linux-x86_64",
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        graph = json.loads(result.stdout)
        args = lib.node_by_output(graph, "$(B)/lib/a.c.o")["cmds"][0]["cmd_args"]
        self.assertIn("-DFROM_CONF", args)

    def test_invalid_ya_conf_is_fatal(self):
        root = self.source_root("[flags\n")
        result = self.make(root)
        self.assertEqual(result.returncode, 1)
        self.assertTrue(result.stderr.startswith(RED + "ya.conf ya.conf: toml:"))

    def test_json_string_escaping(self):
        root = self.source_root('[flags]\nOPENSOURCE = "yes"\n')
        cflags = (
            "'-DX=\t\n\r\b\f\x01a\u2028b\u2029c\udcffd\u00e9' '-DQ=\"' -DB=\\\\ '\udcfe'"
        )
        result = subprocess.run(
            [
                str(lib.AY), "make", "-j0", "-G", "--source-root", str(root),
                "--target-platform", "default-linux-aarch64",
                "--host-platform", "default-linux-x86_64",
                "lib",
            ],
            env=toolchain_free_env(CFLAGS=cflags),
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            timeout=30,
            check=False,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn(
            b'"-DX=\\t\\n\\r\\b\\f\\u0001a\\u2028b\\u2029c\\ufffdd\xc3\xa9",'
            b'"-DQ=\\"","-DB=\\\\","\\ufffd"',
            result.stdout,
        )
        graph = json.loads(result.stdout)
        args = lib.node_by_output(graph, "$(B)/lib/a.c.o")["cmds"][0]["cmd_args"]
        self.assertIn("-DX=\t\n\r\b\f\x01a\u2028b\u2029c\ufffdd\u00e9", args)
        self.assertIn('-DQ="', args)
        self.assertIn("-DB=\\", args)

    def test_large_graph_is_written_in_several_chunks(self):
        big = "-DBIG=" + "x" * (300 << 10)
        root = self.source_root(f'[flags]\nOPENSOURCE = "yes"\nCFLAGS = "{big}"\n')
        result = self.make(
            root,
            "--target-platform", "default-linux-aarch64",
            "--host-platform", "default-linux-x86_64",
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        graph = json.loads(result.stdout)
        args = lib.node_by_output(graph, "$(B)/lib/a.c.o")["cmds"][0]["cmd_args"]
        self.assertIn(big, args)
        self.assertEqual(
            [node["kv"]["p"] for node in graph["graph"]],
            ["CP", "CC", "CC", "AR"],
        )
        archive = lib.node_by_output(graph, "$(B)/lib/liblib.a")
        self.assertEqual(archive["inputs"], ["$(B)/lib/a.c.o", "$(B)/lib/b.c.o"])

    def test_ya_make_syntax_error_is_fatal(self):
        root = self.source_root('[flags]\nOPENSOURCE = "yes"\n')
        (root / "lib/ya.make").write_text("LIBRARY()\nSRCS(a.c\nEND()\n")
        result = self.make(root)
        self.assertEqual(result.returncode, 1)
        self.assertEqual(
            result.stderr,
            fatal('lib/ya.make:3:4: unexpected \'(\' inside macro call "SRCS"'),
        )


if __name__ == "__main__":
    unittest.main(verbosity=2)
