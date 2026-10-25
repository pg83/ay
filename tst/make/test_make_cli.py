import json
import platform
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

from tst import lib
from tst.cli import RED, fatal, run_ay, toolchain_free_env


LIBRARY_YA_MAKE = (
    "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(a.c b.c)\nEND()\n"
)


class MakeCliTest(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory(prefix="ay-cli-test-")
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)

    def source_root(self, ya_conf):
        root = self.root / "src"
        (root / "lib").mkdir(parents=True)
        lib.seed_root(root)
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

    def test_make_without_platforms_uses_host(self):
        root = self.source_root('[flags]\nOPENSOURCE = "yes"\n')
        result = self.make(root)
        self.assertEqual(result.returncode, 0, result.stderr)
        graph = json.loads(result.stdout)
        compile_node = lib.node_by_output(graph, "$(B)/lib/a.c.o")
        # ay names the host after GOOS and its ISA: x86_64, aarch64 on Linux,
        # arm64 on macOS, which are also what Python reports for the host.
        self.assertEqual(compile_node["platform"], f"default-{sys.platform}-{platform.machine()}")

    def test_make_names_the_host_after_its_os_and_isa(self):
        root = self.source_root('[flags]\nOPENSOURCE = "yes"\n')
        for words, expected in (
            ("host-os=linux host-arch=amd64", "default-linux-x86_64"),
            ("host-os=linux host-arch=arm64", "default-linux-aarch64"),
            ("host-os=darwin host-arch=arm64", "default-darwin-arm64"),
            ("host-os=ios host-arch=arm64", "default-ios-arm64"),
        ):
            with self.subTest(words=words):
                result = self.make(root, env=toolchain_free_env(AY_CHAOS=words))
                self.assertEqual(result.returncode, 0, result.stderr)
                graph = json.loads(result.stdout)
                self.assertEqual(lib.node_by_output(graph, "$(B)/lib/a.c.o")["platform"], expected)

    def test_make_on_an_unsupported_host_isa_fails(self):
        root = self.source_root('[flags]\nOPENSOURCE = "yes"\n')
        result = self.make(root, env=toolchain_free_env(AY_CHAOS="host-os=linux host-arch=riscv64"))
        self.assertEqual(result.returncode, 1)
        self.assertEqual(result.stdout, "")
        self.assertEqual(result.stderr, fatal(
            "ymake conf: unsupported platforms host=DEFAULT-LINUX-RISCV64 target=DEFAULT-LINUX-RISCV64 for tool c++"
        ))

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
        # Linux caps one argument at 128 KiB: a ya.conf flag this size would not
        # pass on ymake_conf.py's command line, a module flag does.
        big = "-DBIG=" + "x" * (300 << 10)
        root = self.source_root('[flags]\nOPENSOURCE = "yes"\n')
        (root / "lib/ya.make").write_text(LIBRARY_YA_MAKE.replace("SRCS(", f"CFLAGS({big})\nSRCS("))
        result = self.make(
            root, "--sandboxing",
            "--target-platform", "default-linux-aarch64",
            "--host-platform", "default-linux-x86_64",
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        graph = json.loads(result.stdout)
        args = lib.node_by_output(graph, "$(B)/lib/a.c.o")["cmds"][0]["cmd_args"]
        self.assertIn(big, args)
        self.assertEqual(
            [node["kv"]["p"] for node in graph["graph"]],
            ["CC", "CC", "AR"],
        )
        archive = lib.node_by_output(graph, "$(B)/lib/liblib.a")
        self.assertEqual(archive["inputs"], [
            "$(B)/lib/a.c.o", "$(B)/lib/b.c.o", "$(S)/build/scripts/link_lib.py",
        ])

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
