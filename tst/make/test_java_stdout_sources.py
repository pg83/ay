from tst.make.lib import MakeTest, PROJECT


class JavaStdoutSourcesTest(MakeTest):
    def check_generated_source(self):
        graph = self.graph()
        generated = self.node(graph, "Generated.java")
        compiled = self.node(graph, "answer.jar")
        self.assertIn(f"$(B)/{PROJECT}/Generated.java", compiled["inputs"])
        self.assertIn(generated["uid"], compiled["deps"])

    def test_python_stdout_is_compiled_as_java_source(self):
        self.write("ya.make", "JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nRUN_PYTHON3(generate.py STDOUT Generated.java)\nEND()\n")
        self.write("generate.py", 'print("public class Generated {}")\n')
        self.check_generated_source()

    def test_program_stdout_is_compiled_as_java_source(self):
        self.write("ya.make", f"JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nRUN_PROGRAM({PROJECT}/tool STDOUT Generated.java)\nEND()\n")
        self.write("tool/ya.make", "PROGRAM(generator)\nSRCS(main.cpp)\nEND()\n")
        self.write("tool/main.cpp", '#include <cstdio>\nint main() { std::puts("public class Generated {}"); }\n')
        self.check_generated_source()
