from tst.make.lib import MakeTest, PROJECT


class GeneratorIndirectInputsTest(MakeTest):
    def test_swig_generator_inputs_keep_transitive_and_explicit_headers(self):
        self.write("ya.make", f"LIBRARY(answer)\nPYTHON3_ADDINCL()\nADDINCL({PROJECT})\nPEERDIR(contrib/tools/swig/Lib/python)\nRUN_PROGRAM({PROJECT}/generator IN interface.swg value.h schema.proto STDOUT generated.cpp OUTPUT_INCLUDES value.h)\nEND()\n")
        self.write("interface.swg", '%module answer\n%include "value.h"\n')
        self.write("value.h", "#pragma once\n#define VALUE 42\n")
        self.write("schema.proto", 'syntax = "proto3";\nmessage Answer {}\n')
        self.write("generator/ya.make", "PROGRAM(generator)\nSRCS(main.cpp)\nEND()\n")
        self.write("generator/main.cpp", '#include <cstdio>\nint main() { std::puts("#include \\"value.h\\"\\nint generated() { return VALUE; }"); }\n')
        graph = self.graph()
        generated = self.node(graph, "generated.cpp")
        for source in ("interface.swg", "value.h", "schema.proto"):
            self.assertIn(f"$(S)/{PROJECT}/{source}", generated["inputs"])
        compiled = self.node(graph, "generated.cpp.o")
        self.assertIn(generated["uid"], compiled["deps"])
        self.assertIn(f"$(S)/{PROJECT}/value.h", compiled["inputs"])
        self.assertIn("$(S)/contrib/tools/python3/Include/Python.h", compiled["inputs"])
