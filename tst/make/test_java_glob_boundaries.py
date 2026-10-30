from tst.make.lib import MakeTest, PROJECT


class JavaGlobBoundariesTest(MakeTest):
    def test_intermediate_wildcards_skip_files(self):
        self.write("ya.make", "JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nJAVA_SRCS(src/*/*.java)\nEND()\n")
        self.write("src/README.txt", "sources\n")
        self.write("src/pkg/Answer.java", "package pkg; public class Answer {}\n")
        node = self.node(self.graph(), "answer.jar")
        self.assertIn(f"$(S)/{PROJECT}/src/pkg/Answer.java", node["inputs"])
        self.assertNotIn(f"$(S)/{PROJECT}/src/README.txt", node["inputs"])

    def test_source_rooted_exclusions_remove_matching_sources(self):
        self.write("ya.make", f"JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nJAVA_SRCS(src/*.java EXCLUDE ${{ARCADIA_ROOT}}/{PROJECT}/src/Ignored.java)\nEND()\n")
        self.write("src/Answer.java", "public class Answer {}\n")
        self.write("src/Ignored.java", "public class Ignored {}\n")
        node = self.node(self.graph(), "answer.jar")
        self.assertIn(f"$(S)/{PROJECT}/src/Answer.java", node["inputs"])
        self.assertNotIn(f"$(S)/{PROJECT}/src/Ignored.java", node["inputs"])

    def test_source_root_dot_pattern_does_not_match_module_sources(self):
        self.write("ya.make", f"JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nJAVA_SRCS(${{ARCADIA_ROOT}}/./{PROJECT}*/Answer.java)\nEND()\n")
        self.write("Answer.java", "public class Answer {}\n")
        node = self.node(self.graph(), "answer.jar")
        self.assertNotIn(f"$(S)/{PROJECT}/Answer.java", node["inputs"])
