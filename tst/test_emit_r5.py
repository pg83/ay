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

    def test_ragel_inputs_keep_recursive_native_includes(self):
        for extension in ("rl", "rl5"):
            with self.subTest(extension=extension):
                source = "scan." + extension
                files = {
                    "mod/ya.make": "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(" + source + ")\nEND()\n",
                    "mod/" + source: '#include "mod/main.h"\n%%{\n machine Scan;\n include Common "common.rli";\n}%%\n',
                    "mod/common.rli": '#include "mod/native.h"\n%%{\n machine Common;\n include Deep "deep.rli";\n}%%\n',
                    "mod/deep.rli": '%%{\n machine Deep;\n}%%\n',
                    "mod/main.h": '#include "main_detail.h"\n',
                    "mod/main_detail.h": "",
                    "mod/native.h": "",
                }
                lib.tool_program(files, "contrib/tools/ragel5/ragel", "ragel5")
                lib.tool_program(files, "contrib/tools/ragel5/rlgen-cd", "rlgen-cd")
                graph = lib.make(files, "mod", "--dist")
                generator = lib.node_by_output(graph, "$(B)/mod/scan.rl5.cpp")
                self.assertEqual(sorted(generator["inputs"]), sorted([
                    "$(B)/contrib/tools/ragel5/ragel/ragel5",
                    "$(B)/contrib/tools/ragel5/rlgen-cd/rlgen-cd",
                    "$(S)/mod/" + source, "$(S)/mod/common.rli", "$(S)/mod/deep.rli",
                ]))
                compiled = lib.node_by_output(graph, "$(B)/mod/scan.rl5.cpp.o")
                self.assertIn(generator["uid"], compiled["deps"])
                self.assertIn("$(S)/mod/main.h", compiled["inputs"])
                self.assertIn("$(S)/mod/main_detail.h", compiled["inputs"])
                for path in (source, "common.rli", "deep.rli", "native.h"):
                    self.assertNotIn("$(S)/mod/" + path, compiled["inputs"])

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
