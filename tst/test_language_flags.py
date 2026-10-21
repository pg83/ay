import unittest

import lib


class LanguageFlagsTest(unittest.TestCase):
    def test_generated_cxx_suffixes_select_language_flags(self):
        flags = {
            "a.y.cpp": ["-Wno-unused-but-set-variable", "-Wno-deprecated-copy"],
            "b.ypp.cpp": ["-Wno-unused-but-set-variable", "-Wno-deprecated-copy"],
            "c.rl5.cpp": ["-Wno-implicit-fallthrough"],
            "d.rl6.cpp": ["-Wno-implicit-fallthrough"],
            "e.pyx.cpp": ["-Wno-implicit-fallthrough"],
            "f.py.py3.cpp": ["-Wno-implicit-fallthrough"],
            "g.py.py2.cpp": ["-Wno-implicit-fallthrough"],
            "h.pyx.py3.cpp": ["-Wno-implicit-fallthrough"],
            "i.pyx.py2.cpp": ["-Wno-implicit-fallthrough"],
            "j.swg.cxx": ["-Wno-deprecated-declarations"],
            "k.l.cpp": ["-Wno-unused-variable"],
            "l.g4.cpp": ["-Wno-unused-variable"],
            "plain.cpp": [],
            "header.py3.cpp": [],
            "c.pyx.c": [],
        }
        files = {
            "mod/ya.make": (
                "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
                f"RUN_PROGRAM(tools/gen OUT {' '.join(flags)})\nEND()\n"
            ),
        }
        lib.tool_program(files, "tools/gen", "gen")
        graph = lib.make(files, "mod")
        language_flags = set(flag for values in flags.values() for flag in values)
        for filename, expected in flags.items():
            with self.subTest(filename=filename):
                node = lib.node_by_output(graph, f"$(B)/mod/{filename}.o")
                args = node["cmds"][0]["cmd_args"]
                self.assertEqual([arg for arg in args if arg in language_flags], expected)

    def test_language_flags_require_a_supported_compiler(self):
        for compiler in ("CLANG", "CLANG_CL", "GCC", "OTHER"):
            with self.subTest(compiler=compiler):
                files = {
                    "build/ymake_conf.py": lib.ymake_conf(extra=(
                        f"CLANG=no\nCLANG_CL=no\nGCC=no\n{compiler}=yes\n"
                    )),
                    "mod/ya.make": (
                        "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
                        "RUN_PROGRAM(tools/gen OUT gen.y.cpp)\nEND()\n"
                    ),
                }
                lib.tool_program(files, "tools/gen", "gen")
                graph = lib.make(files, "mod")
                node = lib.node_by_output(graph, "$(B)/mod/gen.y.cpp.o")
                self.assertEqual(
                    "-Wno-deprecated-copy" in node["cmds"][0]["cmd_args"],
                    compiler != "OTHER",
                )


if __name__ == "__main__":
    unittest.main(verbosity=2)
