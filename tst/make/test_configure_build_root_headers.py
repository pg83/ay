from tst.make.lib import MakeTest


class ConfigureBuildRootHeadersTest(MakeTest):
    def test_headers_configured_at_the_build_root_keep_their_producer(self):
        name = "__ay_e2e_generated_root__.h"
        self.write("ya.make", f"LIBRARY()\nADDINCL(${{ARCADIA_BUILD_ROOT}}/)\nCONFIGURE_FILE(answer.in ${{ARCADIA_BUILD_ROOT}}/{name})\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.in", "#pragma once\n#define ANSWER 42\n")
        self.write("answer.cpp", f'#include "{name}"\nint answer() {{ return ANSWER; }}\n')
        graph = self.graph()
        copied = [node for node in graph["graph"] if f"$(B)/{name}" in node["outputs"]]
        self.assertEqual(len(copied), 1)
        compiled = self.node(graph, "answer.cpp.o")
        self.assertIn(copied[0]["uid"], compiled["deps"])
        self.assertIn(f"$(B)/{name}", compiled["inputs"])
