import json
import os
import subprocess
import tempfile
import unittest
from pathlib import Path

import lib


def library(body=""):
    return f"LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n{body}END()\n"


def program(name, body=""):
    return (
        f"PROGRAM({name})\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
        f"SRCS(m.cpp)\n{body}END()\n"
    )


def make_raw(files, target, *args):
    with tempfile.TemporaryDirectory(prefix="ay-link-gen-") as directory:
        root = Path(directory)
        (root / ".arcadia.root").touch()
        (root / "ya.conf").write_text(
            '[flags]\nOPENSOURCE = "yes"\n\n[host_platform_flags]\nOPENSOURCE = "yes"\n'
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
        return subprocess.run(
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


def outputs_of(graph, refs):
    by_uid = {node["uid"]: node for node in graph["graph"]}
    return [by_uid[ref]["outputs"][0] for ref in refs]


def link_command(node):
    return next(
        " ".join(cmd["cmd_args"])
        for cmd in node["cmds"]
        if any(arg.endswith("link_exe.py") for arg in cmd["cmd_args"])
    )


BROKEN_PEERS = {
    "nomod/ya.make": "SRCS(a.cpp)\n",
    "nomod/a.cpp": "int a;\n",
    "multi/ya.make": "LIBRARY()\nEND()\nPROGRAM()\nEND()\n",
    "ok/ya.make": library("SRCS(ok.cpp)\n"),
    "ok/ok.cpp": "int ok;\n",
    "app/ya.make": program("app", "PEERDIR(nomod multi ok)\n"),
    "app/m.cpp": "int main(){return 0;}\n",
}


class GenModulesTest(unittest.TestCase):
    def test_peerdir_cycle_is_tolerated_and_reported(self):
        files = {
            "a/ya.make": library("SRCS(a.cpp)\nPEERDIR(b)\n"),
            "a/a.cpp": "int a(){return 1;}\n",
            "b/ya.make": library("SRCS(b.cpp)\nPEERDIR(a)\n"),
            "b/b.cpp": "int b(){return 2;}\n",
            "p/ya.make": program("prog", "PEERDIR(a)\n"),
            "p/m.cpp": "int main(){return 0;}\n",
        }
        result = make_raw(files, "p")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("gen: PEERDIR cycle tolerated at a", result.stderr)
        graph = json.loads(result.stdout)
        link = lib.node_by_output(graph, "$(B)/p/prog")
        command = link_command(link)
        self.assertIn("-Wl,--start-group b/libb.a a/liba.a -Wl,--end-group", command)
        self.assertIn("$(B)/a/liba.a", link["inputs"])
        self.assertIn("$(B)/b/libb.a", link["inputs"])

    def test_missing_module_declaration_fails(self):
        result = make_raw(BROKEN_PEERS, "app")
        self.assertEqual(result.returncode, 1)
        self.assertIn(
            "gen: nomod has no module declaration (PROGRAM/LIBRARY)",
            result.stderr,
        )
        self.assertEqual(result.stdout, "")

    def test_multiple_module_declarations_fail(self):
        result = make_raw(BROKEN_PEERS, "multi")
        self.assertEqual(result.returncode, 1)
        self.assertIn(
            "gen: multi declares multiple modules (LIBRARY and PROGRAM); "
            "only one is allowed",
            result.stderr,
        )

    def test_keep_going_skips_failed_peers(self):
        result = make_raw(BROKEN_PEERS, "app", "-k")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn(
            "module-failed: nomod: gen: nomod has no module declaration",
            result.stderr,
        )
        self.assertIn(
            "module-failed: multi: gen: multi declares multiple modules",
            result.stderr,
        )
        graph = json.loads(result.stdout)
        link = lib.node_by_output(graph, "$(B)/app/app")
        self.assertIn("-Wl,--start-group ok/libok.a -Wl,--end-group", link_command(link))
        self.assertEqual(outputs_of(graph, graph["result"]), ["$(B)/app/app"])
        produced = {out for node in graph["graph"] for out in node["outputs"]}
        self.assertNotIn("$(B)/nomod/a.cpp.o", produced)

    def test_peerdir_to_program_fails(self):
        files = dict(BROKEN_PEERS)
        files["app/ya.make"] = program("app", "PEERDIR(ok)\n")
        files["tool/ya.make"] = program("tool", "PEERDIR(app)\n")
        files["tool/m.cpp"] = "int main(){return 0;}\n"
        result = make_raw(files, "tool")
        self.assertEqual(result.returncode, 1)
        self.assertIn(
            "gen: tool peers PROGRAM module app; only LIBRARY peers are linkable",
            result.stderr,
        )

    def test_recurse_discovers_final_targets(self):
        files = {
            "top/ya.make": library("SRCS(t.cpp)\n")
            + "RECURSE(\n  app\n  lib\n  nodir\n)\n"
            + "RECURSE_ROOT_RELATIVE(other/ut)\n",
            "top/t.cpp": "int t;\n",
            "top/app/ya.make": program("app") + "RECURSE(../lib ../app)\n",
            "top/app/m.cpp": "int main(){return 0;}\n",
            "top/lib/ya.make": library("SRCS(l.cpp)\n") + "RECURSE(deep)\n",
            "top/lib/l.cpp": "int l;\n",
            "top/lib/deep/ya.make": program("deep"),
            "top/lib/deep/m.cpp": "int main(){return 0;}\n",
            "top/nodir/readme.txt": "not a module\n",
            "other/ut/ya.make": (
                "UNITTEST_FOR(top/lib)\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
                "SRCS(l.cpp)\nEND()\n"
            ),
            "library/cpp/testing/unittest_main/ya.make": library(),
        }
        graph = lib.make(files, "top")
        self.assertEqual(
            outputs_of(graph, graph["result"]),
            [
                "$(B)/top/libtop.a",
                "$(B)/top/lib/deep/deep",
                "$(B)/top/app/app",
                "$(B)/other/ut/other-ut",
            ],
        )
        unittest_cc = lib.node_by_output(graph, "$(B)/other/ut/__/__/top/lib/l.cpp.o")
        self.assertIn("$(S)/top/lib/l.cpp", unittest_cc["inputs"])
        unittest_link = lib.node_by_output(graph, "$(B)/other/ut/other-ut")
        self.assertIn(
            "-Wl,--start-group top/lib/libtop-lib.a -Wl,--end-group",
            link_command(unittest_link),
        )
        produced = {out for node in graph["graph"] for out in node["outputs"]}
        self.assertIn("$(B)/top/lib/libtop-lib.a", produced)

    def test_autoinclude_roots_append_linters_make_inc(self):
        files = {
            "build/conf/autoincludes.json": '["proj"]\n',
            "proj/linters.make.inc": "CFLAGS(-DLINTED)\n",
            "proj/a/ya.make": library("SRCS(a.cpp)\n"),
            "proj/a/a.cpp": "int a;\n",
            "proj/b/ya.make": program("b", "PEERDIR(proj/a other)\n"),
            "proj/b/m.cpp": "int main(){return 0;}\n",
            "other/ya.make": library("SRCS(o.cpp)\n"),
            "other/o.cpp": "int o;\n",
        }
        graph = lib.make(files, "proj/b")
        for output, linted in (
            ("$(B)/proj/a/a.cpp.o", True),
            ("$(B)/proj/b/m.cpp.o", True),
            ("$(B)/other/o.cpp.o", False),
        ):
            with self.subTest(output=output):
                args = lib.node_by_output(graph, output)["cmds"][0]["cmd_args"]
                self.assertEqual("-DLINTED" in args, linted)


if __name__ == "__main__":
    unittest.main(verbosity=2)
