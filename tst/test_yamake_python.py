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
PYTHON_LIBRARIES = [
    "contrib/libs/python",
    "contrib/tools/python3/Modules/_sqlite",
    "library/cpp/malloc/jemalloc",
    "library/cpp/resource",
    "library/python/import_tracing/constructor",
    "library/python/runtime_py3",
    "library/python/runtime_py3/main",
    "library/python/testing/import_test",
]
PYTHON_TOOLS = [
    "contrib/tools/swig",
    "tools/archiver",
    "tools/py3cc",
    "tools/py3cc/slow",
    "tools/rescompiler",
    "tools/rescompressor",
]
KV_PREFIXES = ("PY_MAIN=", "py/constructors/", "py/no_check_imports/", "resfs/src/resfs/file/")


def python_tree(kind, body, sources):
    files = {"a/ya.make": f"{kind}()\n" + NO_PLATFORM + body + "\nEND()\n"}
    for source in sources:
        files[f"a/{source}"] = "\n"
    for path in PYTHON_LIBRARIES:
        files[f"{path}/ya.make"] = "LIBRARY()\n" + NO_PLATFORM + "END()\n"
    for path in PYTHON_TOOLS:
        lib.tool_program(files, path, path.rsplit("/", 1)[-1])
    return files


def ay_make(files, *args):
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
                *args, "a",
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


def resource_kvs(graph):
    return [
        arg
        for node in graph["graph"]
        for cmd in node["cmds"]
        for arg in cmd["cmd_args"]
        if arg.startswith(KV_PREFIXES) and not arg.endswith(".yapyc3")
    ]


class YaMakePythonTest(unittest.TestCase):
    def make(self, kind, body, sources):
        code, graph, stderr = ay_make(python_tree(kind, body, sources), "-k")
        self.assertEqual(code, 0, stderr)
        return graph

    def test_main_module_resolution(self):
        cases = [
            ("PY3_LIBRARY", "PY_SRCS(MAIN m.py)", ["m.py"], "PY_MAIN=a.m:main"),
            ("PY3_LIBRARY", "PY_SRCS(TOP_LEVEL MAIN m.py)", ["m.py"], "PY_MAIN=m:main"),
            ("PY3_LIBRARY", "PY_SRCS(MAIN sub/m.py=renamed)", ["sub/m.py"], "PY_MAIN=renamed:main"),
            ("PY3_PROGRAM", "PY_SRCS(__main__.py)", ["__main__.py"], "PY_MAIN=a.__main__"),
            ("PY3_PROGRAM", "PY_SRCS(TOP_LEVEL sub/__main__.py)", ["sub/__main__.py"], "PY_MAIN=sub.__main__"),
            ("PY3_LIBRARY", "PY_SRCS(x.py)\nPY_MAIN(pkg/mod)", ["x.py"], "PY_MAIN=pkg.mod:main"),
            ("PY3_LIBRARY", "PY_SRCS(x.py)\nPY_MAIN(pkg.mod:run)", ["x.py"], "PY_MAIN=pkg.mod:run"),
        ]
        for kind, body, sources, expected in cases:
            with self.subTest(body=body):
                kvs = resource_kvs(self.make(kind, body, sources))
                self.assertEqual([kv for kv in kvs if kv.startswith("PY_MAIN=")], [expected])

    def test_constructors_and_import_checks(self):
        kvs = sorted(resource_kvs(self.make(
            "PY3_LIBRARY",
            "PY_SRCS(x.py)\nPY_CONSTRUCTOR(my.mod:init)\nPY_CONSTRUCTOR(my.other)\n"
            "NO_CHECK_IMPORTS(foo.* bar)",
            ["x.py"],
        )))
        self.assertEqual(kvs, [
            "py/constructors/my.mod=init",
            "py/constructors/my.other=init",
            "py/no_check_imports/5obff57yher4ocqaic5cgnlday=foo.* bar",
            "resfs/src/resfs/file/py/a/x.py=a/x.py",
        ])

    def test_all_py_srcs_walks_directories(self):
        sources = [
            "x.py", "y.py", "test_z.py", "z_test.py", "sub/w.py", "sub/deep/v.py",
            "other/o.py", "nested/n.py", "notes.txt",
        ]
        cases = [
            ("ALL_PY_SRCS()", ["py/a/test_z.py", "py/a/x.py", "py/a/y.py", "py/a/z_test.py"]),
            ("ALL_PY_SRCS(NO_TEST_FILES)", ["py/a/x.py", "py/a/y.py"]),
            ("ALL_PY_SRCS(RECURSIVE)", [
                "py/a/other/o.py", "py/a/sub/w.py", "py/a/sub/deep/v.py", "py/a/test_z.py",
                "py/a/x.py", "py/a/y.py", "py/a/z_test.py",
            ]),
            ("ALL_PY_SRCS(TOP_LEVEL sub other)", ["py/sub/w.py", "py/other/o.py"]),
            ("ALL_PY_SRCS(NAMESPACE ns RECURSIVE sub)", ["py/ns/sub/deep/v.py", "py/ns/sub/w.py"]),
            ("ALL_PY_SRCS(missing)", []),
        ]
        for body, expected in cases:
            with self.subTest(body=body):
                files = python_tree("PY3_LIBRARY", body, sources)
                files["a/nested/ya.make"] = "LIBRARY()\n" + NO_PLATFORM + "END()\n"
                code, graph, stderr = ay_make(files, "-k")
                self.assertEqual(code, 0, stderr)
                self.assertEqual([
                    kv[len("resfs/src/resfs/file/"):kv.index("=")]
                    for kv in resource_kvs(graph)
                ], expected)

    def test_cython_and_swig_variants(self):
        graph = self.make("PY3_LIBRARY", (
            "PY_SRCS(\n"
            "  NAMESPACE my.ns\n"
            "  CYTHON_CPP c1.pyx\n"
            "  CYTHON_C c2.pyx\n"
            "  CYTHON_DIRECTIVE language_level=3\n"
            "  CYTHON_C_H c4.pyx\n"
            "  CYTHON_C_API_H c5.pyx=custom.name\n"
            "  TOP_LEVEL\n"
            "  CYTHON_CPP_H c3.pyx\n"
            "  CYTHONIZE_PY CYTHON_CPP cy.py\n"
            "  SWIG_C s.swg\n"
            "  SWIG_CPP s2.swg\n"
            "  stubs.pyi\n"
            "  data.txt=ignored\n"
            ")\n"
            "PY_REGISTER(reg.mod plain)"
        ), ["c1.pyx", "c2.pyx", "c3.pyx", "c4.pyx", "c5.pyx", "cy.py", "s.swg", "s2.swg", "stubs.pyi"])
        cython = [node for node in graph["graph"] if node["kv"]["p"] == "CY"]
        self.assertEqual([node["outputs"] for node in cython], [
            ["$(B)/a/c2.pyx.c"],
            ["$(B)/a/c4.c", "$(B)/a/c4.h"],
            ["$(B)/a/c5.c", "$(B)/a/c5.h", "$(B)/a/c5_api.h"],
            ["$(B)/a/c1.pyx.cpp"],
            ["$(B)/a/cy.py.cpp"],
            ["$(B)/a/c3.cpp", "$(B)/a/c3.h"],
        ])
        module_names = [
            (args[args.index("--module-name") + 1], args[args.index("--init-suffix") + 1])
            for args in (node["cmds"][0]["cmd_args"] for node in cython)
        ]
        self.assertEqual(module_names, [
            ("my.ns.c2", "2my2ns2c2"),
            ("my.ns.c4", "2my2ns2c4"),
            ("custom.name", "6custom4name"),
            ("my.ns.c1", "2my2ns2c1"),
            ("cy", "cy"),
            ("c3", "c3"),
        ])
        for node in cython:
            args = node["cmds"][0]["cmd_args"]
            self.assertEqual(args[args.index("language_level=3") - 1], "-X")
        registered = sorted(
            node["outputs"][0][len("$(B)/a/"):-len(".reg3.cpp")]
            for node in graph["graph"]
            if node["kv"]["p"] == "PY" and node["outputs"][0].endswith(".reg3.cpp")
        )
        self.assertEqual(registered, [
            "c3", "custom.name", "cy", "my.ns.c1", "my.ns.c2", "my.ns.c4", "plain",
            "reg.mod", "s_swg",
        ])
        swig = lib.only_node_by_kind(graph, "SW")
        self.assertEqual(swig["outputs"], ["$(B)/a/s.swg.c", "$(B)/a/s.py"])
        self.assertIn("$(S)/a/stubs.pyi", {
            source for node in graph["graph"] for source in node["inputs"]
        })


if __name__ == "__main__":
    unittest.main(verbosity=2)
