from tst.make.lib import MakeTest, PROJECT


class SharedGlobalLinkerInputsTest(MakeTest):
    def test_global_linker_script_keeps_its_previously_discovered_dependencies(self):
        self.write("ya.make", f"PROGRAM(answer)\nPEERDIR({PROJECT}/lib)\nSRCS(main.cpp GLOBAL ${{ARCADIA_ROOT}}/{PROJECT}/lib/script.ld)\nEND()\n")
        self.write("main.cpp", "int main() { return 0; }\n")
        self.write("lib/ya.make", "LIBRARY()\nSRCS(native.cpp)\nEND()\n")
        self.write("lib/native.cpp", '#include "script.ld"\nint native() { return 42; }\n')
        self.write("lib/script.ld", '#include "dependency.h"\n')
        self.write("lib/dependency.h", "#pragma once\n")
        graph = self.graph()
        link = self.node(graph, "answer")
        self.assertIn(f"$(S)/{PROJECT}/lib/script.ld", link["inputs"])
        self.assertIn(f"$(S)/{PROJECT}/lib/dependency.h", link["inputs"])
