from tst.make.lib import MakeTest


class JavaSourceGlobRootsTest(MakeTest):
    def jar(self, declarations):
        self.write("ya.make", f"JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nJAVA_SRCS(Main.java)\n{declarations}\nEND()\n")
        self.write("Main.java", "public class Main {}\n")
        return self.node(self.graph(), "answer.jar")

    def test_repeated_rooted_separators_keep_matching_sources(self):
        self.write("src/Answer.java", "package src; public class Answer {}\n")
        node = self.jar(f"JAVA_SRCS(SRCDIR ${{ARCADIA_ROOT}}/{self.project_name}// src//*.java)")
        self.assertIn(f"$(S)/{self.project_name}/src/Answer.java", node["inputs"])

    def test_an_unbound_resource_pattern_does_not_remove_ordinary_sources(self):
        node = self.jar("JAVA_SRCS($E2E_RESOURCE_GLOBAL/Unused.java)")
        self.assertIn(f"$(S)/{self.project_name}/Main.java", node["inputs"])
        self.assertFalse(any("E2E_RESOURCE_GLOBAL" in name for name in node["inputs"]))
