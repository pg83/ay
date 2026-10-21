import unittest

import lib


class DeclareInDirsTests(unittest.TestCase):
    def test_rooted_directory_and_pattern_alternatives(self):
        graph = lib.make({
            "mod/ya.make": f"""LIBRARY()
{lib.BARE_MODULE}
ADDINCL(${{ARCADIA_BUILD_ROOT}}/mod)
DECLARE_IN_DIRS(HEADERS *.h|*.cpp DIRS . RECURSIVE SRCDIR ${{ARCADIA_ROOT}}/headers)
RUN_PYTHON3(gen.py \\${{HEADERS_FILES}} IN ${{HEADERS_FILES}} OUT generated.h)
SRCS(main.cpp)
END()
""",
            "mod/main.cpp": '#include "generated.h"\n',
            "mod/gen.py": "",
            "headers/api.h": "",
            "headers/api.cpp": "",
            "headers/sub/more.h": "",
            "headers/skip.txt": "",
        }, "mod")
        node = lib.node_by_output(graph, "$(B)/mod/generated.h")
        expected = ["$(S)/headers/api.cpp", "$(S)/headers/api.h", "$(S)/headers/sub/more.h"]
        for source in expected:
            self.assertIn(source, node["inputs"])
        self.assertEqual(node["cmds"][0]["cmd_args"][2:], expected)
        self.assertNotIn("$(S)/headers/skip.txt", node["inputs"])


if __name__ == "__main__":
    unittest.main()
