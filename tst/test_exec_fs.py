import json
import os
import subprocess
import tempfile
import unittest
from pathlib import Path

import lib


MIB = 1 << 20
LONG_DIR = "d" * 200
LONG_FILE = "long_" + "x" * 80 + ".cpp"


def library(sources, extra=""):
    return (
        "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
        f"{extra}SRCS({' '.join(sources)})\nEND()\n"
    )


class Tree:
    def __init__(self, test, files):
        tmp = tempfile.TemporaryDirectory(prefix="ay-fs-test-")
        test.addCleanup(tmp.cleanup)
        self.src = Path(tmp.name).resolve()
        for relative, content in {
            ".arcadia.root": "",
            "ya.conf": '[flags]\nOPENSOURCE = "yes"\n',
            **files,
        }.items():
            path = self.src / relative
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(content)

    def run(self, *args):
        env = {k: v for k, v in os.environ.items() if k not in lib.TOOLCHAIN_ENV_VARS}
        return subprocess.run(
            [str(lib.AY), "make", "-j0", "-G", "--sandboxing", "--source-root", str(self.src),
             "--host-platform", "default-linux-x86_64", *args],
            env=env,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
            timeout=60,
            check=False,
        )

    def graph(self, *args):
        result = self.run(*args)
        if result.returncode != 0:
            raise AssertionError(f"exit {result.returncode}\n{result.stderr}")
        return json.loads(result.stdout), result.stderr


def cc_nodes(graph):
    return {
        node["outputs"][0]: node
        for node in graph["graph"]
        if node["kv"].get("p") == "CC"
    }


class SourcePathResolutionTest(unittest.TestCase):
    def test_unclean_and_long_relative_sources(self):
        tree = Tree(self, {
            "mod/ya.make": library([
                "../shared/x.cpp", "./local.cpp", "sub/../y.cpp", f"../{LONG_DIR}/{LONG_FILE}",
            ]),
            "shared/x.cpp": "int x(){return 0;}\n",
            "mod/local.cpp": "int local(){return 0;}\n",
            "mod/y.cpp": "int y(){return 0;}\n",
            f"{LONG_DIR}/{LONG_FILE}": "int l(){return 0;}\n",
        })
        graph, _ = tree.graph("mod")
        nodes = cc_nodes(graph)
        self.assertEqual(sorted(nodes), sorted([
            "$(B)/mod/__/shared/x.cpp.o",
            "$(B)/mod/local.cpp.o",
            "$(B)/mod/y.cpp.o",
            f"$(B)/mod/__/{LONG_DIR}/{LONG_FILE}.o",
        ]))
        self.assertIn("$(S)/shared/x.cpp", nodes["$(B)/mod/__/shared/x.cpp.o"]["inputs"])
        self.assertIn("$(S)/mod/y.cpp", nodes["$(B)/mod/y.cpp.o"]["inputs"])
        self.assertIn(
            f"$(S)/{LONG_DIR}/{LONG_FILE}",
            nodes[f"$(B)/mod/__/{LONG_DIR}/{LONG_FILE}.o"]["inputs"],
        )

    def test_unclean_paths_under_srcdir_includes_and_addincl(self):
        long_header = f"../{LONG_DIR}/{LONG_FILE[:-4]}.h"
        tree = Tree(self, {
            "mod/ya.make": library(
                ["../shared/x.cpp", f"../{LONG_DIR}/{LONG_FILE}", "../nodir/w.cpp", "nodir/z.cpp"],
                "ADDINCL(mod/.. mod/sub/..)\nSRCDIR(other)\n",
            ),
            "other/.keep": "",
            "shared/x.cpp": f'#include "../shared/h.h"\n#include "{long_header}"\nint x(){{return 0;}}\n',
            "shared/h.h": "#pragma once\n",
            f"{LONG_DIR}/{LONG_FILE}": "int l(){return 0;}\n",
            f"{LONG_DIR}/{LONG_FILE[:-4]}.h": "#pragma once\n",
            "mod/sub/.keep": "",
            "build/scripts": "not a directory\n",
        })
        graph, _ = tree.graph("-k", "mod")
        nodes = cc_nodes(graph)
        self.assertEqual(sorted(nodes), sorted([
            "$(B)/mod/__/shared/x.cpp.o",
            f"$(B)/mod/__/{LONG_DIR}/{LONG_FILE}.o",
            "$(B)/mod/__/nodir/w.cpp.o",
            "$(B)/mod/_/nodir/z.cpp.o",
        ]))
        shared = nodes["$(B)/mod/__/shared/x.cpp.o"]
        self.assertEqual(shared["inputs"], [
            "$(S)/shared/x.cpp",
            "$(S)/shared/h.h",
            f"$(S)/{LONG_DIR}/{LONG_FILE[:-4]}.h",
        ])
        self.assertIn("-I$(S)/mod/..", shared["cmds"][0]["cmd_args"])
        self.assertEqual(nodes["$(B)/mod/_/nodir/z.cpp.o"]["inputs"], ["$(S)/mod/nodir/z.cpp"])

    def test_include_below_missing_directory_is_unresolved(self):
        tree = Tree(self, {
            "mod/ya.make": library(["a.cpp"]),
            "mod/a.cpp": '#include "nodir/x.h"\nint a(){return 0;}\n',
        })
        result = tree.run("mod")
        self.assertEqual(result.returncode, 1)
        self.assertIn('unresolved include "nodir/x.h"', result.stderr)

    def test_missing_source_root(self):
        tree = Tree(self, {})
        result = subprocess.run(
            [str(lib.AY), "make", "-j0", "--source-root", str(tree.src / "absent"), "mod"],
            stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, timeout=30, check=False,
        )
        self.assertEqual(result.returncode, 1)
        self.assertIn(f"open source root {tree.src / 'absent'}: no such file or directory", result.stderr)


class DirectoryListingTest(unittest.TestCase):
    def test_many_directories_and_large_directories(self):
        dirs = [f"inc/d{i:03d}" for i in range(800)]
        files = {
            "mod/ya.make": library(["a.cpp"], f"ADDINCL({' '.join(dirs)})\n"),
            "mod/a.cpp": '#include "wanted.h"\nint a(){return 0;}\n',
            "crowd/wanted.h": "#pragma once\n",
        }
        for i in range(2500):
            files[f"crowd/entry_{i:05d}_{'n' * 30}.txt"] = ""
        files["mod/ya.make"] = library(["a.cpp"], f"ADDINCL({' '.join(dirs)} crowd)\n")
        tree = Tree(self, files)
        for directory in dirs:
            (tree.src / directory).mkdir(parents=True)
        graph, stderr = tree.graph("mod")
        self.assertEqual(stderr, "")
        node = cc_nodes(graph)["$(B)/mod/a.cpp.o"]
        args = node["cmds"][0]["cmd_args"]
        self.assertIn("-I$(S)/inc/d000", args)
        self.assertIn("-I$(S)/inc/d799", args)
        self.assertIn("$(S)/crowd/wanted.h", node["inputs"])


class LargeFileTest(unittest.TestCase):
    def test_scripts_larger_than_one_read_chunk(self):
        padding = "#" + "p" * 98 + "\n"
        big = "import helper\n" + padding * (MIB * 3 // 2 // 100)
        exact = "import helper\n"
        exact += padding * ((MIB - len(exact)) // 100)
        exact += "#" * (MIB - len(exact) - 1) + "\n"
        self.assertEqual(len(exact), MIB)
        tree = Tree(self, {
            "build/platform/res/ya.make": (
                "RESOURCES_LIBRARY()\nDECLARE_EXTERNAL_RESOURCE(RES sbr:1)\nEND()\n"
            ),
            "build/scripts/fetch_from_sandbox.py": "import big_one\n",
            "build/scripts/fetch_from_mds.py": "import big_two, exact\n",
            "build/scripts/big_one.py": big,
            "build/scripts/big_two.py": big,
            "build/scripts/exact.py": exact,
            "build/scripts/helper.py": "",
        })
        graph, _ = tree.graph("build/platform/res")
        fetch = lib.node_by_output(graph, "$(B)/resources/RES")
        self.assertEqual(fetch["inputs"], [
            "$(S)/build/mapping.conf.json",
            "$(S)/build/scripts/fetch_from_sandbox.py",
            "$(S)/build/scripts/big_one.py",
            "$(S)/build/scripts/helper.py",
            "$(S)/build/scripts/fetch_from_mds.py",
            "$(S)/build/scripts/big_two.py",
            "$(S)/build/scripts/exact.py",
            "$(S)/build/scripts/helper.py",
        ])


if __name__ == "__main__":
    unittest.main(verbosity=2)
