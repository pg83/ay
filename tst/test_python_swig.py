import unittest

import lib


STUB_LIBRARY = "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n"

PEER_STUBS = (
    "contrib/libs/python",
    "library/cpp/resource",
)

TOOLS = (
    "contrib/tools/swig",
    "tools/archiver",
    "tools/py3cc",
    "tools/py3cc/slow",
    "tools/rescompiler",
    "tools/rescompressor",
)

SWIG_LIB = "contrib/tools/swig/Lib"
SWIG_IMPLICIT = [
    f"$(S)/{SWIG_LIB}/python/python.swg",
    f"$(S)/{SWIG_LIB}/perl5.swg",
    f"$(S)/{SWIG_LIB}/java.swg",
    f"$(S)/{SWIG_LIB}/go.swg",
    f"$(S)/{SWIG_LIB}/swig.swg",
]


def python_base():
    files = {f"{path}/ya.make": STUB_LIBRARY for path in PEER_STUBS}
    for path in TOOLS:
        lib.tool_program(files, path, path.rsplit("/", 1)[-1])
    for name in ("swig.swg", "go.swg", "java.swg", "perl5.swg"):
        files[f"{SWIG_LIB}/{name}"] = ""
    files[f"{SWIG_LIB}/python/python.swg"] = '%include "pyhead.swg"\n'
    files[f"{SWIG_LIB}/python/pyhead.swg"] = ""
    return files


def args(node):
    return node["cmds"][0]["cmd_args"]


class SwigTest(unittest.TestCase):
    def test_swig_c_sources_generate_wrapper_and_python_module(self):
        files = python_base()
        files.update({
            "lib/ya.make": (
                "PY3_LIBRARY()\n"
                "PY_SRCS(SWIG_C wrap.swg sub/other.swg SWIG_CPP skipped.swg a.py)\n"
                "END()\n"
            ),
            "lib/a.py": "",
            "lib/wrap.swg": (
                "%module wrap\n"
                "\n"
                '%include "lib/common.i"\n'
                '%import "lib/types.i"\n'
                '%insert(runtime) "lib/rt.swg"\n'
                '%insert "lib/no_section.swg"\n'
                '%insert(unterminated "lib/broken.swg"\n'
                "%insert (header) %{\n"
                '%include "lib/inside_block.i"\n'
                "%}\n"
                "%header %{\n"
                '#include "lib/impl.h"\n'
                "  #include \"lib/impl2.h\"\n"
                "static int x;\n"
                "%}\n"
                "%includefile\n"
                "%include <std_string.i>\n"
            ),
            "lib/sub/other.swg": "",
            "lib/skipped.swg": "",
            "lib/common.i": '%include "lib/nested.i"\n',
            "lib/nested.i": "",
            "lib/types.i": "",
            "lib/rt.swg": "",
            "lib/impl.h": "",
            "lib/impl2.h": "",
            f"{SWIG_LIB}/std_string.i": "",
        })
        graph = lib.make(files, "lib")

        swig = lib.node_by_output(graph, "$(B)/lib/wrap.swg.c")
        self.assertEqual(swig["kv"]["p"], "SW")
        self.assertEqual(swig["outputs"], ["$(B)/lib/wrap.swg.c", "$(B)/lib/wrap.py"])
        self.assertEqual(args(swig), [
            "$(B)/contrib/tools/swig/swig",
            "-I$(B)", "-I$(S)",
            f"-I$(S)/{SWIG_LIB}/python", f"-I$(S)/{SWIG_LIB}",
            "-python", "-module", "wrap", "-interface", "wrap_swg",
            "-o", "$(B)/lib/wrap.swg.c", "$(S)/lib/wrap.swg",
        ])
        self.assertEqual(swig["inputs"][:2], ["$(B)/contrib/tools/swig/swig", "$(S)/lib/wrap.swg"])
        for expected in (
            "$(S)/lib/common.i", "$(S)/lib/nested.i", "$(S)/lib/types.i", "$(S)/lib/rt.swg",
            f"$(S)/{SWIG_LIB}/std_string.i", f"$(S)/{SWIG_LIB}/python/pyhead.swg",
            *SWIG_IMPLICIT,
        ):
            self.assertIn(expected, swig["inputs"])
        for unexpected in (
            "$(S)/lib/no_section.swg", "$(S)/lib/broken.swg", "$(S)/lib/inside_block.i",
            "$(S)/lib/impl.h",
        ):
            self.assertNotIn(unexpected, swig["inputs"])
        swig_tool = lib.node_by_output(graph, "$(B)/contrib/tools/swig/swig")
        self.assertIn(swig_tool["uid"], swig["deps"])

        compiled = lib.node_by_output(graph, "$(B)/lib/wrap.swg.c.o")
        for expected in ("$(B)/lib/wrap.swg.c", "$(S)/lib/impl.h", "$(S)/lib/impl2.h", "$(S)/lib/wrap.swg"):
            self.assertIn(expected, compiled["inputs"])
        self.assertIn(swig["uid"], compiled["deps"])

        other = lib.node_by_output(graph, "$(B)/lib/sub/other.swg.c")
        self.assertEqual(args(other)[5:12], [
            "-python", "-module", "other", "-interface", "other_swg",
            "-o", "$(B)/lib/sub/other.swg.c",
        ])
        lib.node_by_output(graph, "$(B)/lib/_/sub/other.swg.c.o")
        self.assertFalse([
            node for node in graph["graph"]
            if any("skipped" in output for output in node["outputs"])
        ])

        pyc = lib.node_by_output(graph, "$(B)/lib/wrap.py.yapyc3")
        self.assertEqual(args(pyc)[-3:], [
            "lib/wrap.py-", "$(B)/lib/wrap.py", "$(B)/lib/wrap.py.yapyc3",
        ])
        self.assertIn("$(B)/lib/wrap.swg.c", pyc["inputs"])
        self.assertIn("$(S)/lib/common.i", pyc["inputs"])
        packs = [
            node for node in graph["graph"]
            if node["kv"].get("p") == "PR" and "$(B)/lib/wrap.py" in node["inputs"]
        ]
        self.assertEqual(len(packs), 1)
        self.assertIn("resfs/src/resfs/file/py/lib/wrap.py=lib/wrap.py", args(packs[0]))
        self.assertIn(
            "resfs/src/resfs/file/py/lib/wrap.py.yapyc3=lib/wrap.py.yapyc3", args(packs[0]))

        reg = lib.node_by_output(graph, "$(B)/lib/lib.wrap_swg.reg3.cpp")
        self.assertEqual(args(reg)[-2:], ["lib.wrap_swg", "$(B)/lib/lib.wrap_swg.reg3.cpp"])
        archive = lib.node_by_output(graph, "$(B)/lib/libpy3lib.a")
        self.assertEqual(archive["inputs"][:2], ["$(B)/lib/wrap.swg.c.o", "$(B)/lib/_/sub/other.swg.c.o"])

    def test_top_level_swig_module_names(self):
        files = python_base()
        files.update({
            "lib/ya.make": (
                "PY3_LIBRARY()\n"
                "PY_SRCS(TOP_LEVEL SWIG_C wrap.swg renamed.swg=pkg.custom)\n"
                "END()\n"
            ),
            "lib/wrap.swg": "",
            "lib/renamed.swg": "",
        })
        graph = lib.make(files, "lib")
        lib.node_by_output(graph, "$(B)/lib/wrap_swg.reg3.cpp")
        renamed = lib.node_by_output(graph, "$(B)/lib/custom.swg.c")
        self.assertEqual(renamed["outputs"], ["$(B)/lib/custom.swg.c", "$(B)/lib/custom.py"])
        self.assertIn("-module", args(renamed))
        self.assertEqual(args(renamed)[args(renamed).index("-module") + 1], "custom")
        lib.node_by_output(graph, "$(B)/lib/pkg.custom_swg.reg3.cpp")
        packs = [
            node for node in graph["graph"]
            if node["kv"].get("p") == "PR" and "$(B)/lib/wrap.py" in node["inputs"]
        ]
        self.assertEqual(len(packs), 1)
        self.assertIn("resfs/src/resfs/file/py/wrap.py=lib/wrap.py", args(packs[0]))


if __name__ == "__main__":
    unittest.main(verbosity=2)
