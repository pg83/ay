from tst.make.lib import MakeTest, PROJECT


class VariantGeneratorIncludesTest(MakeTest):
    def test_additional_variant_outputs_keep_their_induced_inputs(self):
        self.write("ya.make", f"PROTO_LIBRARY(answer)\nONLY_TAGS(CPP_PROTO PY3_PROTO)\nPYTHON3_ADDINCL()\nADDINCL({PROJECT})\nADDINCL(FOR swig {PROJECT} FOR swig contrib/tools/swig/Lib FOR swig contrib/tools/swig/Lib/python)\nIF(PY3_PROTO)\nRUN_PROGRAM({PROJECT}/generator IN input.swg OUT_NOAUTO common.tmp OUT extra.cpp)\nELSE()\nRUN_PROGRAM({PROJECT}/generator IN input.swg OUT_NOAUTO common.tmp)\nENDIF()\nSRCS(answer.proto)\nEND()\n")
        self.write("answer.proto", 'syntax = "proto3"; message Answer {}\n')
        self.write("input.swg", '%module answer\n%include "dependency.h"\n')
        self.write("dependency.h", "#pragma once\n")
        self.write("generator/ya.make", "PROGRAM(generator)\nSRCS(main.cpp)\nEND()\n")
        self.write("generator/main.cpp", "int main() { return 0; }\n")
        graph = self.graph()
        generated = self.node(graph, "extra.cpp")
        self.assertEqual(generated["uid"], self.node(graph, "common.tmp")["uid"])
        self.assertIn(f"$(S)/{PROJECT}/input.swg", generated["inputs"])
        self.assertIn(f"$(S)/{PROJECT}/dependency.h", generated["inputs"])
        compiled = self.node(graph, "extra.cpp.py3.o")
        self.assertIn(generated["uid"], compiled["deps"])
