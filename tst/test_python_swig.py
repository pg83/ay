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
    files[f"{SWIG_LIB}/python/ya.make"] = (
        "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
        f"ADDINCL(GLOBAL FOR swig {SWIG_LIB}/python GLOBAL FOR swig {SWIG_LIB})\nEND()\n"
    )
    files[f"{SWIG_LIB}/python/pyhead.swg"] = ""
    return files


def args(node):
    return node["cmds"][0]["cmd_args"]


class SwigTest(unittest.TestCase):
    def test_python_swig_registration_appends_native_init_flags(self):
        files = python_base()
        files.update({
            "lib/ya.make": (
                "PY3_LIBRARY()\nSRC(early.c)\nPY_SRCS(SWIG_C first.swg second.swg)\n"
                "CFLAGS(-DKEEP=1)\nSRCS(native.c)\nEND()\n"
            ),
            "lib/early.c": "",
            "lib/native.c": "",
            "lib/first.swg": "%module first\n",
            "lib/second.swg": "%module second\n",
        })
        graph = lib.make(files, "lib")
        first = ["-DPyInit_first_swg=PyInit_3lib9first_swg", "-Dinit_module_first_swg=init_module_3lib9first_swg"]
        second = ["-DPyInit_second_swg=PyInit_3lib10second_swg", "-Dinit_module_second_swg=init_module_3lib10second_swg"]
        cases = {
            "early.c.o": [], "native.c.o": first + second,
            "first.swg.c.o": first + second, "second.swg.c.o": first + second,
            "lib.first_swg.reg3.cpp.o": [], "lib.second_swg.reg3.cpp.o": first,
        }
        for output, expected in cases.items():
            with self.subTest(output=output):
                compiled = lib.node_by_output(graph, "$(B)/lib/" + output)
                selected = [a for a in args(compiled) if a.startswith(("-DPyInit_", "-Dinit_module_"))]
                self.assertEqual(selected, expected)
                self.assertIn("-DKEEP=1", args(compiled))

    def test_generated_input_parser_initializes_swig_metadata(self):
        files = python_base()
        files.update({
            "lib/ya.make": (
                "PY3_LIBRARY()\nPY_SRCS(SWIG_C wrap.swg)\n"
                "CONFIGURE_FILE(${ARCADIA_BUILD_ROOT}/lib/wrap.swg.c bridge.h)\nSRCS(main.cpp)\nEND()\n"
            ),
            "lib/wrap.swg": '%header %{\n#include "nested.h"\n%}\n',
            "lib/nested.h": "",
            "lib/main.cpp": '#include "bridge.h"\n',
        })
        graph = lib.make(files, "lib")
        compiled = lib.node_by_output(graph, "$(B)/lib/main.cpp.o")
        self.assertIn("$(S)/lib/nested.h", compiled["inputs"])
        producers = [n for n in graph["graph"] if "$(B)/lib/wrap.swg.c" in n["outputs"]]
        self.assertEqual(len(producers), 1)

    def test_generic_runner_resolves_native_swig_include_paths(self):
        files = python_base()
        lib.tool_program(files, "tools/gen", "gen")
        files.update({
            "lib/ya.make": (
                f"LIBRARY()\n{lib.BARE_MODULE}"
                f"ADDINCL(FOR swig {SWIG_LIB}/python FOR swig {SWIG_LIB} FOR swig custom)\n"
                "RUN_PROGRAM(tools/gen IN wrap.swg OUT gen.cpp)\nEND()\n"
            ),
            "lib/wrap.swg": '%include <custom.i>\n',
            "custom/custom.i": '%include "nested.i"\n',
            "custom/nested.i": "",
        })
        graph = lib.make(files, "lib")
        generated = lib.node_by_output(graph, "$(B)/lib/gen.cpp")
        for header in ("custom/custom.i", "custom/nested.i"):
            self.assertIn(f"$(S)/{header}", generated["inputs"])

    def test_generic_runner_passes_induced_includes_through_nested_swig_inputs(self):
        files = python_base()
        files.update({
            "lib/ya.make": (
                f"LIBRARY()\n{lib.BARE_MODULE}"
                f"ADDINCL(FOR swig {SWIG_LIB}/python FOR swig {SWIG_LIB})\n"
                "RUN_PROGRAM(contrib/tools/swig IN wrap.swg OUT wrapper.cpp)\nEND()\n"
            ),
            "lib/wrap.swg": '%include "lib/common.i"\n',
            "lib/common.i": '%include "lib/nested.i"\n',
            "lib/nested.i": '%header %{\n#include "lib/public.h"\n%}\n',
            "lib/public.h": '#include "detail.h"\n',
            "lib/detail.h": '#include "public.h"\n',
        })
        graph = lib.make(files, "lib")
        generated = lib.node_by_output(graph, "$(B)/lib/wrapper.cpp")
        compiled = lib.node_by_output(graph, "$(B)/lib/wrapper.cpp.o")
        for name in ("common.i", "nested.i"):
            self.assertIn(f"$(S)/lib/{name}", generated["inputs"])
            self.assertNotIn(f"$(S)/lib/{name}", compiled["inputs"])
        for name in ("public.h", "detail.h"):
            self.assertNotIn(f"$(S)/lib/{name}", generated["inputs"])
            self.assertIn(f"$(S)/lib/{name}", compiled["inputs"])
        self.assertIn(generated["uid"], compiled["deps"])

    def test_swig_c_sources_generate_wrapper_and_python_module(self):
        files = python_base()
        files.update({
            "lib/ya.make": (
                "PY3_LIBRARY()\n"
                "PY_SRCS(SWIG_C wrap.swg sub/other.swg SWIG_CPP cpp_wrap.swg a.py)\n"
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
            "lib/cpp_wrap.swg": "",
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
        for expected in ('$(B)/lib/wrap.swg.c', '$(S)/lib/impl.h', '$(S)/lib/impl2.h',):
            self.assertIn(expected, compiled["inputs"])
        self.assertNotIn('$(S)/lib/wrap.swg', compiled['inputs'])
        self.assertIn(swig["uid"], compiled["deps"])

        other = lib.node_by_output(graph, "$(B)/lib/sub/other.swg.c")
        self.assertEqual(args(other)[5:12], [
            "-python", "-module", "other", "-interface", "other_swg",
            "-o", "$(B)/lib/sub/other.swg.c",
        ])
        lib.node_by_output(graph, "$(B)/lib/_/sub/other.swg.c.o")
        cpp = lib.node_by_output(graph, "$(B)/lib/cpp_wrap.swg.cpp")
        self.assertIn("-c++", args(cpp))
        self.assertIn(cpp["uid"], lib.node_by_output(graph, "$(B)/lib/cpp_wrap.swg.cpp.o")["deps"])

        pyc = lib.node_by_output(graph, "$(B)/lib/wrap.py.zsw2.yapyc3")
        self.assertEqual(args(pyc)[-3:], [
            "lib/wrap.py-", "$(B)/lib/wrap.py", "$(B)/lib/wrap.py.zsw2.yapyc3",
        ])
        self.assertIn("$(B)/lib/wrap.swg.c", pyc["inputs"])
        self.assertNotIn("$(S)/lib/common.i", pyc["inputs"])
        # wrap.swg.c is the main output of the SW node, a path only: the
        # devtools_yamaker _libarchive.py.gxhp.yapyc3 lists 4 inputs, none of
        # the headers of _libarchive.swg.c.
        self.assertNotIn("$(S)/lib/impl.h", pyc["inputs"])
        packs = [
            node for node in graph["graph"]
            if node["kv"].get("p") == "PR" and "$(B)/lib/wrap.py" in node["inputs"]
        ]
        self.assertEqual(len(packs), 1)
        self.assertIn("$(B)/lib/wrap.swg.c", packs[0]["inputs"])
        self.assertIn(swig["uid"], packs[0]["deps"])
        self.assertNotIn("$(S)/lib/impl.h", packs[0]["inputs"])
        self.assertNotIn("$(S)/lib/common.i", packs[0]["inputs"])
        self.assertIn("resfs/src/resfs/file/py/lib/wrap.py=lib/wrap.py", args(packs[0]))
        self.assertIn(
            "resfs/src/resfs/file/py/lib/wrap.py.yapyc3=lib/wrap.py.zsw2.yapyc3", args(packs[0]))

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
