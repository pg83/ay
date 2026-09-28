import unittest

import lib


class ContextParserTest(unittest.TestCase):
    """Files without a registered parser are parsed with the parser of the
    closure root: ragel, lex and yasm roots here."""

    def test_unregistered_includes_use_root_parser(self):
        files = {
            "mod/ya.make": (
                "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
                "RUN_PROGRAM(tools/gen x.rl y.l OUT out.h IN x.rl y.l)\n"
                "SRCS(a.asm use.cpp)\nEND()\n"
            ),
            "mod/x.rl": '%%{\n  include "rdefs.inc";\n}%%\n',
            "mod/rdefs.inc": '%%{\n  include "r2.rl";\n}%%\n',
            "mod/r2.rl": "",
            "mod/y.l": '#include "ldefs.inc"\n',
            "mod/ldefs.inc": '#include "l2.h"\n',
            "mod/l2.h": "",
            "mod/a.asm": '%include "adefs.inc"\n',
            "mod/adefs.inc": '%include "a2.asm"\n',
            "mod/a2.asm": "",
            "mod/use.cpp": '#include "out.h"\n',
        }
        lib.tool_program(files, "tools/gen", "gen")
        lib.tool_program(files, "contrib/tools/yasm", "yasm")
        graph = lib.make(files, "mod", "--target-platform", "default-linux-x86_64")

        run = lib.node_by_output(graph, "$(B)/mod/out.h")
        self.assertEqual(run["inputs"][:3], [
            "$(B)/tools/gen/gen", "$(S)/mod/x.rl", "$(S)/mod/y.l",
        ])
        self.assertEqual(sorted(run["inputs"][3:]), [
            "$(S)/mod/l2.h", "$(S)/mod/ldefs.inc", "$(S)/mod/rdefs.inc",
        ])

        yasm = lib.node_by_output(graph, "$(B)/mod/a.o")
        self.assertEqual(yasm["inputs"][:2], [
            "$(B)/contrib/tools/yasm/yasm", "$(S)/mod/a.asm",
        ])
        self.assertEqual(sorted(yasm["inputs"][2:]), [
            "$(S)/mod/a2.asm", "$(S)/mod/adefs.inc",
        ])


if __name__ == "__main__":
    unittest.main(verbosity=2)
