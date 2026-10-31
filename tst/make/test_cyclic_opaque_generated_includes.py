from tst.make.lib import MakeTest, PROJECT


class CyclicOpaqueGeneratedIncludesTest(MakeTest):
    def test_cyclic_headers_keep_the_primary_output_of_an_opaque_generated_include(self):
        self.write("ya.make", f"LIBRARY()\nADDINCL(${{ARCADIA_BUILD_ROOT}}/{PROJECT})\nRUN_PYTHON3(generate.py OUT_NOAUTO first.tmp generated.bin)\nSRCS(answer.cpp)\nEND()\n")
        self.write("generate.py", 'from pathlib import Path\nfor name in ("first.tmp", "generated.bin"):\n    Path(name).write_bytes(b"")\n')
        self.write("answer.cpp", '#include "first.h"\nint answer() { return 42; }\n')
        self.write("first.h", '#pragma once\n#include "second.h"\n')
        self.write("second.h", '#pragma once\n#include "first.h"\n#include "generated.bin"\n')
        graph = self.graph()
        generated = self.node(graph, "generated.bin")
        compiled = self.node(graph, "answer.cpp.o")
        self.assertIn(generated["uid"], compiled["deps"])
        for name in ("first.tmp", "generated.bin"):
            self.assertIn(f"$(B)/{PROJECT}/{name}", compiled["inputs"])
