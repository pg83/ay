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


class Ragel6Test(unittest.TestCase):
    def test_ragel6_sources_native_includes_and_output_names(self):
        files = {
            "mod/ya.make": (
                "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
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
            "$(S)/mod/commented.rl",
            "$(S)/mod/common.rl",
            "$(S)/mod/common2.rl",
            "$(S)/mod/hdr.h",
            "$(S)/mod/scan.rl6",
        ])
        scan_compile = lib.node_by_output(graph, "$(B)/mod/scan.rl6.cpp.o")
        self.assertIn("-Wno-implicit-fallthrough", scan_compile["cmds"][0]["cmd_args"])
        self.assertIn("$(S)/mod/common2.rl", scan_compile["inputs"])

        nested = lib.node_by_output(graph, "$(B)/mod/_/sub/other.rl6.cpp")
        self.assertEqual(nested["inputs"], [RAGEL6, "$(S)/mod/sub/other.rl6"])
        lib.node_by_output(graph, "$(B)/mod/_/_/sub/other.rl6.cpp.o")

        lexer = lib.node_by_output(graph, "$(B)/mod/lex.cpp")
        self.assertEqual(lexer["cmds"][0]["cmd_args"][-3:], [
            "-o", "$(B)/mod/lex.cpp", "$(S)/mod/lex.cpp.rl6",
        ])
        lib.node_by_output(graph, "$(B)/mod/lex.cpp.o")

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


if __name__ == "__main__":
    unittest.main(verbosity=2)
