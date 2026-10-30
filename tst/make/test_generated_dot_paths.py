from tst.make.lib import MakeTest, PROJECT


class GeneratedDotPathsTest(MakeTest):
    def test_generated_python_sources_normalize_dot_prefixes(self):
        self.write("ya.make", "PY3_LIBRARY()\nCOPY_FILE(template.py generated.py)\nPY_SRCS(./generated.py)\nEND()\n")
        self.write("template.py", "VALUE = 42\n")
        graph = self.graph()
        copied = self.node(graph, "generated.py")
        compiled = self.node(graph, "generated.py.pccg.yapyc3")
        self.assertIn(f"$(B)/{PROJECT}/generated.py", compiled["inputs"])
        self.assertIn(copied["uid"], compiled["deps"])

    def test_contextual_generated_sources_normalize_local_include_paths(self):
        self.write("ya.make", f"LIBRARY()\nADDINCL(${{ARCADIA_BUILD_ROOT}}/{PROJECT})\nCOPY_FILE_WITH_CONTEXT(template.cpp generated.cpp)\nCOPY_FILE(header.in generated.h)\nSRCS(generated.cpp)\nEND()\n")
        self.write("template.cpp", '#include "./generated.h"\nint answer() { return 42; }\n')
        self.write("header.in", "#pragma once\n")
        graph = self.graph()
        compiled = self.node(graph, "generated.cpp.o")
        self.assertIn(f"$(B)/{PROJECT}/generated.h", compiled["inputs"])
        self.assertIn(self.node(graph, "generated.h")["uid"], compiled["deps"])
