import unittest

import lib


BISON_SKELETONS = [
    "m4sugar/foreach.m4",
    "m4sugar/m4sugar.m4",
    "skeletons/bison.m4",
    "skeletons/c++-skel.m4",
    "skeletons/c++.m4",
    "skeletons/c-like.m4",
    "skeletons/c-skel.m4",
    "skeletons/c.m4",
    "skeletons/glr.cc",
    "skeletons/lalr1.cc",
    "skeletons/location.cc",
    "skeletons/stack.hh",
    "skeletons/variant.hh",
    "skeletons/yacc.c",
]
LIBRARY_HEAD = "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
BISON_ENV = {
    "ARCADIA_ROOT_DISTBUILD": "$(S)",
    "BISON_PKGDATADIR": "$(S)/contrib/tools/bison/data",
    "M4": "$(B)/contrib/tools/m4/m4",
}


def bison_files(body, sources):
    files = {
        "mod/ya.make": LIBRARY_HEAD + body + "END()\n",
        "build/scripts/preprocess.py": "",
        "build/induced/by_bison/ya.make": (
            "LIBRARY()\nNO_UTIL()\nNO_RUNTIME()\nEND()\n"
        ),
        **sources,
    }
    for skeleton in BISON_SKELETONS:
        files[f"contrib/tools/bison/data/{skeleton}"] = ""
    lib.tool_program(files, "contrib/tools/bison", "bison")
    lib.tool_program(files, "contrib/tools/m4", "m4")
    return files


def flex_files(srcs, sources):
    files = {
        "mod/ya.make": LIBRARY_HEAD + f"SRCS({srcs})\nEND()\n",
        "util/system/compiler.h": "",
        **sources,
    }
    lib.tool_program(files, "contrib/tools/flex-old", "flex")
    return files


class BisonTest(unittest.TestCase):
    def test_cpp_grammar_runs_bison_then_header_preprocessor(self):
        graph = lib.make(bison_files("SRCS(gram.y sub/lex.ypp)\n", {
            "mod/gram.y": '%{\n#include "local.h"\n%}\n%%\n',
            "mod/local.h": "",
            "mod/sub/lex.ypp": "%%\n",
        }), "mod")

        node = lib.node_by_output(graph, "$(B)/mod/gram.y.cpp")
        self.assertEqual(node["kv"], {"p": "YC", "pc": "light-green"})
        self.assertEqual(node["outputs"], ["$(B)/mod/gram.h", "$(B)/mod/gram.y.cpp"])
        self.assertEqual(node["cmds"][0]["cmd_args"], [
            "$(B)/contrib/tools/bison/bison", "-v",
            "--defines=$(B)/mod/gram.h",
            "-o", "$(B)/mod/gram.y.cpp",
            "$(S)/mod/gram.y",
        ])
        self.assertEqual(node["cmds"][0]["env"], BISON_ENV)
        self.assertEqual(node["cmds"][1]["cmd_args"][1:], [
            "$(S)/build/scripts/preprocess.py", "$(B)/mod/gram.h",
        ])
        self.assertEqual(node["env"], BISON_ENV)
        self.assertEqual(node["inputs"][:4], [
            "$(B)/contrib/tools/bison/bison",
            "$(B)/contrib/tools/m4/m4",
            "$(S)/mod/gram.y",
            "$(S)/build/scripts/preprocess.py",
        ])
        for skeleton in BISON_SKELETONS:
            self.assertIn(f"$(S)/contrib/tools/bison/data/{skeleton}", node["inputs"])

        compile_node = lib.node_by_output(graph, "$(B)/mod/gram.y.cpp.o")
        args = compile_node["cmds"][0]["cmd_args"]
        self.assertIn("-Wno-unused-but-set-variable", args)
        self.assertIn("-Wno-deprecated-copy", args)
        self.assertEqual(args[-1], "$(B)/mod/gram.y.cpp")
        for path in ("$(B)/mod/gram.h", "$(S)/mod/local.h", "$(S)/mod/gram.y",
                     "$(S)/contrib/tools/bison/data/skeletons/lalr1.cc"):
            self.assertIn(path, compile_node["inputs"])

        nested = lib.node_by_output(graph, "$(B)/mod/_/sub/lex.ypp.cpp")
        self.assertEqual(nested["outputs"], [
            "$(B)/mod/sub/lex.h", "$(B)/mod/_/sub/lex.ypp.cpp",
        ])
        self.assertIn("--defines=$(B)/mod/sub/lex.h", nested["cmds"][0]["cmd_args"])
        nested_compile = lib.node_by_output(graph, "$(B)/mod/_/_/sub/lex.ypp.cpp.o")
        self.assertIn("$(B)/mod/sub/lex.h", nested_compile["inputs"])

    def test_c_grammar_skips_preprocessor_and_passes_flags(self):
        graph = lib.make(bison_files(
            "BISON_GEN_C()\nBISON_FLAGS(-Wno-other --report=all)\nSRCS(gram.y)\n",
            {"mod/gram.y": '%{\n#include "local.h"\n%}\n%%\n', "mod/local.h": ""},
        ), "mod")

        node = lib.node_by_output(graph, "$(B)/mod/gram.y.c")
        self.assertEqual(len(node["cmds"]), 1)
        self.assertEqual(node["cmds"][0]["cmd_args"], [
            "$(B)/contrib/tools/bison/bison", "-v", "-Wno-other", "--report=all",
            "--defines=$(B)/mod/gram.h",
            "-o", "$(B)/mod/gram.y.c",
            "$(S)/mod/gram.y",
        ])
        self.assertEqual(node["inputs"], [
            "$(B)/contrib/tools/bison/bison",
            "$(B)/contrib/tools/m4/m4",
            "$(S)/mod/gram.y",
        ])

        compile_node = lib.node_by_output(graph, "$(B)/mod/gram.y.c.o")
        self.assertNotIn("-Wno-deprecated-copy", compile_node["cmds"][0]["cmd_args"])
        self.assertEqual(sorted(compile_node["inputs"]), sorted([
            "$(B)/mod/gram.y.c", "$(B)/mod/gram.h",
            "$(S)/mod/local.h", "$(S)/mod/gram.y",
        ]))

    def test_bison_gen_cpp_restores_default_extension(self):
        graph = lib.make(bison_files(
            "BISON_GEN_C()\nBISON_GEN_CPP()\nSRCS(gram.y)\n",
            {"mod/gram.y": "%%\n"},
        ), "mod")
        node = lib.node_by_output(graph, "$(B)/mod/gram.y.cpp")
        self.assertEqual(len(node["cmds"]), 2)


class FlexTest(unittest.TestCase):
    def test_lex_sources_include_parser_and_outputs(self):
        scanner = (
            '// #include "commented.h"\n'
            "%{\n"
            '#include "local.h" // trailing comment\n'
            "#include <mod/sysinc.h>\n"
            "#include bare\n"
            '#include a b "late.h"\n'
            "#include 'q.h';\n"
            '#include ""\n'
            '#include "unterminated\n'
            '#include <mixed"\n'
            "%}\n"
        )
        graph = lib.make(flex_files("scan.l empty.lex sub/scan2.lpp", {
            "mod/scan.l": scanner,
            "mod/local.h": "",
            "mod/q.h": "",
            "mod/sysinc.h": "",
            "mod/empty.lex": "%%\n",
            "mod/sub/scan2.lpp": "%%\n",
        }), "mod")

        node = lib.node_by_output(graph, "$(B)/mod/scan.l.cpp")
        self.assertEqual(node["kv"], {"p": "LX", "pc": "yellow"})
        self.assertEqual(node["cmds"][0]["cmd_args"], [
            "$(B)/contrib/tools/flex-old/flex",
            "-o$(B)/mod/scan.l.cpp",
            "$(S)/mod/scan.l",
        ])
        self.assertEqual(node["cmds"][0]["env"], {"ARCADIA_ROOT_DISTBUILD": "$(S)"})
        self.assertEqual(node["inputs"][:2], [
            "$(B)/contrib/tools/flex-old/flex", "$(S)/mod/scan.l",
        ])
        self.assertEqual(set(node["inputs"]), {
            "$(B)/contrib/tools/flex-old/flex",
            "$(S)/mod/scan.l",
            "$(S)/mod/local.h",
            "$(S)/mod/q.h",
            "$(S)/mod/sysinc.h",
            "$(S)/util/system/compiler.h",
        })
        self.assertEqual(node["foreign_deps"]["tool"], node["deps"])

        compile_node = lib.node_by_output(graph, "$(B)/mod/scan.l.cpp.o")
        self.assertIn("-Wno-unused-variable", compile_node["cmds"][0]["cmd_args"])

        empty = lib.node_by_output(graph, "$(B)/mod/empty.lex.cpp")
        self.assertEqual(empty["inputs"], [
            "$(B)/contrib/tools/flex-old/flex",
            "$(S)/mod/empty.lex",
            "$(S)/util/system/compiler.h",
        ])
        empty_compile = lib.node_by_output(graph, "$(B)/mod/empty.lex.cpp.o")
        self.assertNotIn("-Wno-unused-variable", empty_compile["cmds"][0]["cmd_args"])

        nested = lib.node_by_output(graph, "$(B)/mod/_/sub/scan2.lpp.cpp")
        self.assertEqual(nested["cmds"][0]["cmd_args"][1], "-o$(B)/mod/_/sub/scan2.lpp.cpp")
        lib.node_by_output(graph, "$(B)/mod/_/_/sub/scan2.lpp.cpp.o")


if __name__ == "__main__":
    unittest.main(verbosity=2)
