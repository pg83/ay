from tst.make.lib import MakeTest


class JavaSourceExcludeSeparatorsTest(MakeTest):
    def test_repeated_separators_in_source_exclusions_still_exclude_the_file(self):
        self.write("ya.make", "JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nJAVA_SRCS(src/*.java EXCLUDE src//Ignored.java)\nEND()\n")
        self.write("src/Answer.java", "package src; public class Answer {}\n")
        self.write("src/Ignored.java", "package src; public class Ignored {}\n")
        compiled = self.node(self.graph(), "answer.jar")
        self.assertIn(f"$(S)/{self.project_name}/src/Answer.java", compiled["inputs"])
        self.assertNotIn(f"$(S)/{self.project_name}/src/Ignored.java", compiled["inputs"])
