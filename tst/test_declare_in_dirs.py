import unittest

import lib


class DeclareInDirsTests(unittest.TestCase):
    def test_root_module_directory_keeps_source_paths(self):
        for srcdir in ("", " SRCDIR ${ARCADIA_ROOT}"):
            with self.subTest(srcdir=srcdir):
                files = {
                    "ya.make": f"""LIBRARY()
{lib.BARE_MODULE}
ADDINCL(${{BINDIR}})
DECLARE_IN_DIRS(H *.h DIRS .{srcdir})
RUN_PROGRAM(tools/generator \\${{H_FILES}} IN ${{H_FILES}} OUT generated.h)
SRCS(main.cpp)
END()
""",
                    "api.h": "",
                    "main.cpp": '#include "generated.h"\n',
                }
                lib.tool_program(files, "tools/generator", "generator")
                graph = lib.make(files, ".")
                node = lib.node_by_output(graph, "$(B)/generated.h")
                expected = "$(S)/./api.h" if srcdir else "$(S)/api.h"
                self.assertEqual(node["cmds"][0]["cmd_args"][1:], [expected])

    def test_rooted_directory_and_pattern_alternatives(self):
        graph = lib.make({
            "mod/ya.make": f"""LIBRARY()
{lib.BARE_MODULE}
ADDINCL(${{ARCADIA_BUILD_ROOT}}/mod)
DECLARE_IN_DIRS(HEADERS *.h|*.cpp DIRS . api.h missing RECURSIVE SRCDIR ${{ARCADIA_ROOT}}/headers)
RUN_PYTHON3(gen.py \\${{HEADERS_FILES}} IN ${{HEADERS_FILES}} OUT generated.h)
SRCS(main.cpp)
END()
""",
            "mod/main.cpp": '#include "generated.h"\n',
            "mod/gen.py": "",
            "headers/api.h": "",
            "headers/api.cpp": "",
            "headers/first/next.h": "",
            "headers/first/deep/last.h": "",
            "headers/sub/more.h": "",
            "headers/skip.txt": "",
        }, "mod")
        node = lib.node_by_output(graph, "$(B)/mod/generated.h")
        expected = [
            "$(S)/headers/api.cpp", "$(S)/headers/api.h",
            "$(S)/headers/first/next.h", "$(S)/headers/sub/more.h",
            "$(S)/headers/first/deep/last.h",
        ]
        for source in expected:
            self.assertIn(source, node["inputs"])
        self.assertEqual(node["cmds"][0]["cmd_args"][2:], expected)
        self.assertNotIn("$(S)/headers/skip.txt", node["inputs"])


if __name__ == "__main__":
    unittest.main()
