import json
import os
import random
import re
import subprocess
import tempfile
import unittest
from pathlib import Path

import lib


ANSI = re.compile(r"\x1b\[[0-9;]*m")
NO_PLATFORM = "NO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
KIB = 1024


def ay(*args, env=None):
    return subprocess.run(
        [str(lib.AY), *map(str, args)],
        env=env,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
        timeout=30,
        check=False,
    )


def ay_make(files, *args, platform="default-linux-aarch64", env_extra=None):
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
        env.update(env_extra or {})
        result = ay(
            "make", "-j0", "-G", "--sandboxing",
            "--source-root", root,
            "--target-platform", platform,
            "--host-platform", "default-linux-x86_64",
            *args, "a",
            env=env,
        )
        graph = json.loads(result.stdout) if result.returncode == 0 else None
        return result.returncode, graph, ANSI.sub("", result.stderr).strip()


def library():
    return {
        "a/ya.make": "LIBRARY()\n" + NO_PLATFORM + "SRCS(x.cpp y.c)\nEND()\n",
        "a/x.cpp": "int x;\n",
        "a/y.c": "int y;\n",
    }


def compile_args(graph, output):
    return lib.node_by_output(graph, output)["cmds"][0]["cmd_args"]


def after_compilation_dir(args, count):
    start = args.index("/tmp") + 1
    return args[start:start + count]


def human_bytes(size):
    if size < KIB:
        return f"{size} B"
    div, exp = KIB, 0
    value = size // KIB
    while value >= KIB:
        div *= KIB
        exp += 1
        value //= KIB
    return f"{size / div:.2f} {'KMGTPE'[exp]}iB"


class YaMakePlatformTest(unittest.TestCase):
    def test_compiler_flags_from_the_environment_are_split_like_a_shell(self):
        code, graph, stderr = ay_make(library(), env_extra={
            "CFLAGS": "-DA 'b c' \"d e\"  f\\ g\t-Dq'u'\"v\" tail\\",
            "CXXFLAGS": "  -DCXX  ",
        })
        self.assertEqual(code, 0, stderr)
        environment_flags = ["-DA", "b c", "d e", "f g", "-Dquv", "tail\\"]
        cpp = compile_args(graph, "$(B)/a/x.cpp.o")
        start = cpp.index("-DA")
        self.assertEqual(cpp[start:start + 6], environment_flags)
        self.assertIn("-DCXX", cpp)
        c = compile_args(graph, "$(B)/a/y.c.o")
        start = c.index("-DA")
        self.assertEqual(c[start:start + 6], environment_flags)
        self.assertNotIn("-DCXX", c)

    def test_unsupported_target_architectures(self):
        for platform, isa in (("default-linux-riscv64", "riscv64"),):
            with self.subTest(platform=platform):
                code, _, stderr = ay_make(library(), platform=platform)
                self.assertEqual((code, stderr), (1, f'compileFlagBundleFor: unsupported platform ISA "{isa}"'))

    def test_debug_info_and_optimization_flags(self):
        code, graph, stderr = ay_make(library(), "-r", platform="default-linux-x86_64")
        self.assertEqual(code, 0, stderr)
        self.assertEqual(after_compilation_dir(compile_args(graph, "$(B)/a/x.cpp.o"), 4), ["-pipe", "-m64", "-O3", "-fno-common"])

        compressed = library()
        compressed["build/ymake_conf.py"] = "debug_info_flags.append('-gz=zstd')\n"
        code, graph, stderr = ay_make(compressed)
        self.assertEqual(code, 0, stderr)
        self.assertEqual(
            after_compilation_dir(compile_args(graph, "$(B)/a/x.cpp.o"), 5),
            ["-pipe", "-g", "-gz=zstd", "-fdebug-default-version=4", "-ggnu-pubnames"],
        )
        code, graph, stderr = ay_make(compressed, "-r")
        self.assertEqual(code, 0, stderr)
        self.assertEqual(
            after_compilation_dir(compile_args(graph, "$(B)/a/x.cpp.o"), 4),
            ["-pipe", "-g", "-fdebug-default-version=4", "-ggnu-pubnames"],
        )

        code, graph, stderr = ay_make(library(), platform="default-darwin-x86_64")
        self.assertEqual(code, 0, stderr)
        args = compile_args(graph, "$(B)/a/x.cpp.o")
        self.assertEqual(args[1], "--target=x86_64-darwin-gnu")
        self.assertEqual(after_compilation_dir(args, 5), ["-pipe", "-m64", "-g", "-fdebug-default-version=4", "-fno-common"])

    def test_partial_command_lists_matching_subcommands(self):
        result = ay("dev")
        self.assertEqual(result.returncode, 2)
        usage = ANSI.sub("", result.stderr)
        self.assertIn("usage: ay [global-flags] dev <subcommand> [args]", usage)
        self.assertIn("dev cas:", usage)
        self.assertNotIn("\n  make:", usage)

    def test_cas_analyze_reports_human_readable_sizes(self):
        rng = random.Random(7)
        sizes = [100, 5000, 2 * KIB * KIB]
        with tempfile.TemporaryDirectory(prefix="ay-cas-test-") as directory:
            for index, size in enumerate(sizes):
                Path(directory, f"blob{index}").write_bytes(rng.randbytes(size))
            everything = ay("dev", "cas", "analyze", directory)
            self.assertEqual(everything.returncode, 0, everything.stderr)
            self.assertIn(f"min-len {human_bytes(0)})", everything.stdout)
            self.assertIn("  files            3   (>= min-len)", everything.stdout)
            self.assertIn(f"  total size       {human_bytes(sum(sizes))}   ", everything.stdout)
            large = ay("dev", "cas", "analyze", "--min-len=2048", directory)
            self.assertEqual(large.returncode, 0, large.stderr)
            self.assertIn(f"min-len {human_bytes(2048)})", large.stdout)
            self.assertIn("  files            2   (>= min-len)", large.stdout)
            self.assertIn(f"  total size       {human_bytes(sum(sizes[1:]))}   ", large.stdout)

    def test_normalize_keeps_one_copy_of_a_duplicated_root(self):
        def node(uid, kind, output, deps=(), inputs=()):
            return {
                "uid": uid, "kv": {"p": kind}, "outputs": [output], "deps": list(deps),
                "inputs": list(inputs), "cmds": [], "tags": [], "requirements": {},
                "target_properties": {}, "platform": "linux", "env": {},
            }

        link = node("u_ld", "LD", "$(B)/pkg/app/app", deps=["u_cc"], inputs=["$(B)/pkg/app/main.o"])
        compile_node = node("u_cc", "CC", "$(B)/pkg/app/main.o", inputs=["$(S)/pkg/app/main.c"])
        with tempfile.TemporaryDirectory(prefix="ay-dump-test-") as directory:
            raw = Path(directory, "raw.json")
            out = Path(directory, "normalized.jsonl")
            raw.write_text(json.dumps({"conf": {}, "graph": [link, link, compile_node], "result": []}))
            lib.run("dev", "dump", "normalize", "--in", raw, "--target", "pkg/app", "--out", out)
            kinds = sorted(json.loads(line)["kv"]["p"] for line in out.read_text().splitlines())
        self.assertEqual(kinds, ["CC", "LD"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
