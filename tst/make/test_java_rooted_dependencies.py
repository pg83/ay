from tst.make.lib import MakeTest, PROJECT


class JavaRootedDependenciesTest(MakeTest):
    def test_source_root_external_dependencies_are_jar_inputs(self):
        self.write("ya.make", f"JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nJAVA_SRCS(Answer.java)\nJAVA_EXTERNAL_DEPENDENCIES(${{ARCADIA_ROOT}}/{PROJECT}/value.txt)\nEND()\n")
        self.write("Answer.java", "public class Answer {}\n")
        self.write("value.txt", "42\n")
        node = self.node(self.graph(), "answer.jar")
        self.assertIn(f"$(S)/{PROJECT}/value.txt", node["inputs"])
