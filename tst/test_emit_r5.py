import unittest

import lib


class EmitRagel5Test(unittest.TestCase):
    def graph(self, build_flag):
        files = {
            "kernel/urlnorm/ya.make": (
                "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
                "SRCS(urlhashval.rl)\nEND()\n"
            ),
            "kernel/urlnorm/urlhashval.rl": "main := 'a';\n",
        }
        lib.tool_program(files, "contrib/tools/ragel5/ragel", "ragel5")
        lib.tool_program(files, "contrib/tools/ragel5/rlgen-cd", "rlgen-cd")
        return lib.make(files, "kernel/urlnorm", build_flag)

    def test_rlgen_mode_follows_build_type(self):
        for build_flag, expected_mode in (("--debug", "-T0"), ("--release", "-G2")):
            with self.subTest(build_flag=build_flag):
                node = lib.only_node_by_kind(self.graph(build_flag), "R5")
                self.assertEqual(node["cmds"][1]["cmd_args"][1], expected_mode)
                self.assertEqual(node["outputs"], [
                    "$(B)/kernel/urlnorm/urlhashval.rl.tmp",
                    "$(B)/kernel/urlnorm/urlhashval.rl5.cpp",
                ])

    def test_compile_takes_the_c_includes_and_the_main_output_not_the_source(self):
        # devtools_ymake_bin/yabs references: urlhashval.rl5.cpp.o lists the
        # main output urlhashval.rl.tmp and the C includes of the .rl, never
        # the .rl itself.
        files = {
            "m/ya.make": "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(p.rl)\nEND()\n",
            "m/p.rl": '#include "m/x.h"\nmain := \'a\';\n',
            "m/x.h": "",
        }
        lib.tool_program(files, "contrib/tools/ragel5/ragel", "ragel5")
        lib.tool_program(files, "contrib/tools/ragel5/rlgen-cd", "rlgen-cd")
        graph = lib.make(files, "m")
        self.assertEqual(
            sorted(lib.node_by_output(graph, "$(B)/m/p.rl5.cpp.o")["inputs"]),
            ["$(B)/m/p.rl.tmp", "$(B)/m/p.rl5.cpp", "$(S)/m/x.h"],
        )


if __name__ == "__main__":
    unittest.main(verbosity=2)
