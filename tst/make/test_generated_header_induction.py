from tst.make.lib import MakeTest, PROJECT


class GeneratedHeaderInductionTest(MakeTest):
    def test_header_induced_dependencies_reach_cpp_consumers(self):
        self.write("generate.py", "print('#pragma once')\n")
        self.write("native.h", '#pragma once\n#include "leaf.h"\n')
        self.write("leaf.h", "#pragma once\n")
        self.write("answer.cpp", '#include "generated.h"\nint answer() { return 42; }\n')
        for kind in ("h", "h+cpp"):
            with self.subTest(kind=kind):
                self.write("ya.make", f"LIBRARY()\nADDINCL(${{ARCADIA_BUILD_ROOT}}/{PROJECT})\nPREPARE_INDUCED_DEPS(DEPS {kind} ${{ARCADIA_ROOT}}/{PROJECT}/native.h)\nRUN_PYTHON3(generate.py OUT_NOAUTO generated.h INDUCED_DEPS $DEPS)\nSRCS(answer.cpp)\nEND()\n")
                graph = self.graph()
                generated = self.node(graph, "generated.h")
                compiled = self.node(graph, "answer.cpp.o")
                self.assertIn(generated["uid"], compiled["deps"])
                for header in ("native.h", "leaf.h"):
                    self.assertIn(f"$(S)/{PROJECT}/{header}", compiled["inputs"])

    def test_generated_go_inputs_use_the_go_induced_dependency_type(self):
        self.write("ya.make", f"LIBRARY()\nPREPARE_INDUCED_DEPS(DEPS go ${{ARCADIA_ROOT}}/{PROJECT}/native.h)\nRUN_PYTHON3(generate.py OUT_NOAUTO stage.go INDUCED_DEPS $DEPS)\nRUN_PYTHON3(generate.py IN stage.go OUT generated.cpp)\nEND()\n")
        self.write("generate.py", "print('int answer() { return 42; }')\n")
        self.write("native.h", "#pragma once\n")
        generated = self.node(self.graph(), "generated.cpp")
        self.assertIn(f"$(S)/{PROJECT}/native.h", generated["inputs"])
