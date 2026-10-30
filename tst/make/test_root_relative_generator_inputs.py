from tst.make.lib import MakeTest, PROJECT


class RootRelativeGeneratorInputsTest(MakeTest):
    def test_root_relative_generated_inputs_keep_their_producer(self):
        self.write("ya.make", f"LIBRARY()\nCOPY_FILE(template.txt ${{ARCADIA_BUILD_ROOT}}/{PROJECT}/generated.txt)\nRUN_PYTHON3(generate.py IN {PROJECT}/generated.txt STDOUT result.cpp)\nEND()\n")
        self.write("template.txt", "42\n")
        self.write("generate.py", 'print("int answer() { return 42; }")\n')
        graph = self.graph()
        copied = self.node(graph, "generated.txt")
        generated = self.node(graph, "result.cpp")
        self.assertIn(f"$(B)/{PROJECT}/generated.txt", generated["inputs"])
        self.assertIn(copied["uid"], generated["deps"])
