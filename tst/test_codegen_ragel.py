import unittest

import lib


RAGEL6 = "$(B)/contrib/tools/ragel6/ragel6"
SCANNER = (
    '#include "hdr.h"\n'
    "\n"
    "%%{\n"
    "  machine m;\n"
    "\n"
    '  include "common.rl"; # native include\n'
    "  include 'common.rl';\n"
    '  include other "common2.rl";\n'
    '  includes_not "x.rl";\n'
    "  include noquote;\n"
    '  include "unterminated;\n'
    '  # include "commented.rl";\n'
    "}%%\n"
    'include "outside.rl";\n'
)


def inherited_files(extension='asset'):
    files = {
        'mod/ya.make': 'LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(scan.rl6)\nEND()\n',
        'mod/scan.rl6': f'%%{{\n  machine root;\n  include "mid.{extension}";\n}}%%\n',
        f'mod/mid.{extension}': '#include "cpp.h"\n%%{\n  machine mid;\n  include "leaf.rli";\n}%%\n',
        'mod/leaf.rli': '%%{\n  machine leaf;\n}%%\n',
        'mod/cpp.h': '',
    }
    lib.tool_program(files, 'contrib/tools/ragel6', 'ragel6')
    return files


class Ragel6Test(unittest.TestCase):
    def test_inherited_parser_keeps_native_and_induced_include_buckets(self):
        for extension in ('rli', 'asset'):
            with self.subTest(extension=extension):
                graph = lib.make(inherited_files(extension), 'mod', '--sandboxing',
                                 '--target-platform', 'default-linux-x86_64')
                generated = lib.node_by_output(graph, '$(B)/mod/scan.rl6.cpp')
                self.assertCountEqual(generated['inputs'], [
                    RAGEL6, '$(S)/mod/scan.rl6',
                    f'$(S)/mod/mid.{extension}', '$(S)/mod/leaf.rli',
                ])
                tool = lib.node_by_output(graph, RAGEL6)
                self.assertEqual(generated['deps'], [tool['uid']])
                compiled = lib.node_by_output(graph, '$(B)/mod/scan.rl6.cpp.o')
                self.assertEqual(compiled['inputs'], ['$(B)/mod/scan.rl6.cpp'])
                self.assertEqual(compiled['deps'], [generated['uid']])
                archive = lib.node_by_output(graph, '$(B)/mod/libmod.a')
                self.assertCountEqual(archive['inputs'], [
                    '$(B)/mod/scan.rl6.cpp.o', '$(S)/build/scripts/link_lib.py',
                ])
                self.assertEqual(archive['deps'], [compiled['uid']])

    def test_inherited_parser_reports_missing_native_include(self):
        files = inherited_files()
        del files['mod/leaf.rli']
        result = lib.make_process(files, 'mod', '--sandboxing',
                                  '--target-platform', 'default-linux-x86_64')
        self.assertEqual(result.returncode, 1, result.stderr)
        self.assertIn('leaf.rli', result.stderr)
        self.assertIn('mid.asset', result.stderr)

    def test_ragel6_sources_native_includes_and_output_names(self):
        files = {
            "mod/ya.make": (
                "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
                "ADDINCL(${ARCADIA_BUILD_ROOT}/mod)\n"
                "SET(RAGEL6_FLAGS -CG1 -L)\n"
                "SRCS(scan.rl6 sub/other.rl6 gen.h.rl6 lex.cpp.rl6 use.cpp)\n"
                "END()\n"
            ),
            "mod/scan.rl6": SCANNER,
            "mod/hdr.h": "",
            "mod/common.rl": "",
            "mod/common2.rl": "",
            "mod/commented.rl": "",
            "mod/sub/other.rl6": "%%{ machine o; }%%\n",
            "mod/gen.h.rl6": "",
            "mod/lex.cpp.rl6": "",
            "mod/use.cpp": '#include "gen.h"\n',
        }
        lib.tool_program(files, "contrib/tools/ragel6", "ragel6")
        graph = lib.make(files, "mod")

        scan = lib.node_by_output(graph, "$(B)/mod/scan.rl6.cpp")
        self.assertEqual(scan["kv"], {"p": "R6", "pc": "yellow"})
        self.assertEqual(scan["cmds"][0]["cmd_args"], [
            RAGEL6, "-CG1", "-L", "-L", "-I$(S)",
            "-o", "$(B)/mod/scan.rl6.cpp", "$(S)/mod/scan.rl6",
        ])
        self.assertEqual(scan["inputs"][0], RAGEL6)
        self.assertEqual(sorted(scan["inputs"][1:]), [
            '$(S)/mod/common.rl',
            '$(S)/mod/common2.rl',
            '$(S)/mod/scan.rl6',
        ])
        scan_compile = lib.node_by_output(graph, "$(B)/mod/scan.rl6.cpp.o")
        self.assertIn("-Wno-implicit-fallthrough", scan_compile["cmds"][0]["cmd_args"])
        self.assertNotIn("$(S)/mod/common2.rl", scan_compile["inputs"])

        nested = lib.node_by_output(graph, "$(B)/mod/_/sub/other.rl6.cpp")
        self.assertEqual(nested["inputs"], [RAGEL6, "$(S)/mod/sub/other.rl6"])
        lib.node_by_output(graph, "$(B)/mod/_/_/sub/other.rl6.cpp.o")

        lexer = lib.node_by_output(graph, "$(B)/mod/lex.cpp")
        self.assertEqual(lexer["cmds"][0]["cmd_args"][-3:], [
            "-o", "$(B)/mod/lex.cpp", "$(S)/mod/lex.cpp.rl6",
        ])
        lexer_compile = lib.node_by_output(graph, "$(B)/mod/lex.cpp.o")
        self.assertNotIn("-Wno-implicit-fallthrough", lexer_compile["cmds"][0]["cmd_args"])

        header = lib.node_by_output(graph, "$(B)/mod/gen.h")
        self.assertEqual(header["cmds"][0]["cmd_args"][-3:], [
            "-o", "$(B)/mod/gen.h", "$(S)/mod/gen.h.rl6",
        ])
        self.assertFalse(any(
            output.startswith("$(B)/mod/gen.h.")
            for node in graph["graph"] for output in node["outputs"]
        ))
        use = lib.node_by_output(graph, "$(B)/mod/use.cpp.o")
        self.assertIn(header["uid"], use["deps"])

    def test_default_flags_follow_build_type(self):
        files = {
            "mod/ya.make": (
                "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(scan.rl6)\nEND()\n"
            ),
            "mod/scan.rl6": "%%{ machine m; }%%\n",
        }
        lib.tool_program(files, "contrib/tools/ragel6", "ragel6")
        for build_type, flag in (("--debug", "-CT0"), ("--release", "-CG2")):
            with self.subTest(build_type=build_type):
                graph = lib.make(files, "mod", build_type)
                node = lib.node_by_output(graph, "$(B)/mod/scan.rl6.cpp")
                self.assertEqual(node["cmds"][0]["cmd_args"][:3], [RAGEL6, flag, "-L"])


    def test_distbuild_ragel_inputs_are_native_include_closures(self):
        # Under ya --dist (ymake --add-inputs) the R6 node takes the .rl6 and
        # its ragel includes; the generated source brings its C++ includes.
        files = {
            "mod/ya.make": "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(scan.rl6)\nEND()\n",
            "mod/scan.rl6": SCANNER,
            "mod/hdr.h": "",
            "mod/common.rl": "",
            "mod/common2.rl": "",
            "mod/commented.rl": "",
        }
        lib.tool_program(files, "contrib/tools/ragel6", "ragel6")
        graph = lib.make(files, "mod", "--dist")
        scan = lib.node_by_output(graph, "$(B)/mod/scan.rl6.cpp")
        self.assertEqual(sorted(scan["inputs"][1:]), [
            "$(S)/mod/common.rl",
            "$(S)/mod/common2.rl",
            "$(S)/mod/scan.rl6",
        ])
        compiled = lib.node_by_output(graph, "$(B)/mod/scan.rl6.cpp.o")
        self.assertIn("$(S)/mod/hdr.h", compiled["inputs"])
        self.assertNotIn("$(S)/mod/scan.rl6", compiled["inputs"])
        self.assertNotIn("$(S)/mod/common.rl", compiled["inputs"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
