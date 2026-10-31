from tst.make.lib import MakeTest, PROJECT


class GeneratedSourceModulePathsTest(MakeTest):
    def test_generated_assembler_source_at_the_module_path_keeps_its_output_name(self):
        self.write("ya.make", "RECURSE(answer.S)\n")
        self.write("answer.S/ya.make", f"LIBRARY()\nFROM_SANDBOX(FILE 123456789 OUT_NOAUTO ${{ARCADIA_BUILD_ROOT}}/{PROJECT}/answer.S)\nSRCS(${{ARCADIA_BUILD_ROOT}}/{PROJECT}/answer.S)\nEND()\n")
        graph = self.graph()
        source = self.node(graph, "answer.S")
        compiled = self.node(graph, "answer.S/__/answer.S.o")
        self.assertIn(f"$(B)/{PROJECT}/answer.S", compiled["inputs"])
        self.assertIn(source["uid"], compiled["deps"])
