from tst.make.lib import MakeTest, PROJECT


class JavaToolValidationTest(MakeTest):
    def library_tool(self):
        self.write("tool/ya.make", "JAVA_LIBRARY(generator)\nJDK_VERSION(17)\nJAVA_SRCS(Generator.java)\nEND()\n")
        self.write("tool/Generator.java", "public class Generator {}\n")

    def test_run_program_keeps_a_java_library_tool(self):
        self.library_tool()
        self.write("ya.make", f"JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nRUN_PROGRAM({PROJECT}/tool OUT Generated.java)\nEND()\n")
        self.check_tool_dependency()

    def test_run_java_program_keeps_a_java_library_classpath(self):
        self.library_tool()
        self.write("ya.make", f"JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nRUN_JAVA_PROGRAM(Generator CLASSPATH {PROJECT}/tool OUT Generated.java)\nEND()\n")
        self.check_tool_dependency()

    def check_tool_dependency(self):
        graph = self.graph()
        generated = self.node(graph, "Generated.java")
        tool = self.node(graph, "tool/generator.jar")
        self.assertIn(f"$(B)/{PROJECT}/tool/generator.jar", generated["inputs"])
        self.assertIn(tool["uid"], generated["deps"])
