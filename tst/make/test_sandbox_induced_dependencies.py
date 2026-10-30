from tst.make.lib import MakeTest, PROJECT


class SandboxInducedDependenciesTest(MakeTest):
    def test_downloaded_header_keeps_induced_cpp_dependencies(self):
        self.write("ya.make", f"LIBRARY()\nADDINCL(${{ARCADIA_BUILD_ROOT}}/{PROJECT} {PROJECT})\nPREPARE_INDUCED_DEPS(DEPS h ${{ARCADIA_ROOT}}/{PROJECT}/dependency.h)\nFROM_SANDBOX(FILE 123456789 OUT_NOAUTO generated.h INDUCED_DEPS $DEPS)\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", '#include "generated.h"\nint answer() { return 42; }\n')
        self.write("dependency.h", "#pragma once\n")
        graph = self.graph()
        generated = self.node(graph, "generated.h")
        compiled = self.node(graph, "answer.cpp.o")
        self.assertIn(generated["uid"], compiled["deps"])
        self.assertIn(f"$(S)/{PROJECT}/dependency.h", compiled["inputs"])
