from tst.make.lib import MakeTest, PROJECT


class CopyPlanningContextsTest(MakeTest):
    def test_java_copy_sources_resolve_through_declared_source_directories(self):
        self.write("ya.make", f"JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nSRCDIR({PROJECT}/templates)\nCOPY_FILE(Answer.java Generated.java)\nJAVA_SRCS(Generated.java)\nEND()\n")
        self.write("templates/Answer.java", "public class Generated {}\n")
        graph = self.graph()
        copied = self.node(graph, "Generated.java")
        self.assertIn(f"$(S)/{PROJECT}/templates/Answer.java", copied["inputs"])
        compiled = self.node(graph, "answer.jar")
        self.assertIn(f"$(B)/{PROJECT}/Generated.java", compiled["inputs"])
        self.assertIn(copied["uid"], compiled["deps"])

    def test_generated_copy_sources_keep_the_original_producer(self):
        self.write("ya.make", "LIBRARY()\nRUN_PYTHON3(generate.py OUT_NOAUTO original.txt)\nCOPY_FILE(original.txt copied.txt)\nRESOURCE(copied.txt /copy)\nEND()\n")
        self.write("generate.py", 'from pathlib import Path\nPath("original.txt").write_text("42\\n")\n')
        graph = self.graph()
        original = self.node(graph, "original.txt")
        copied = self.node(graph, "copied.txt")
        self.assertIn(f"$(B)/{PROJECT}/original.txt", copied["inputs"])
        self.assertIn(original["uid"], copied["deps"])

    def test_copy_destinations_cannot_have_distinct_sources(self):
        self.write("ya.make", "LIBRARY()\nCOPY_FILE(first.txt shared.txt)\nCOPY_FILE(second.txt shared.txt)\nRESOURCE(shared.txt /copy)\nEND()\n")
        self.write("first.txt", "first\n")
        self.write("second.txt", "second\n")
        self.assertNotEqual(self.make().returncode, 0)

    def test_identical_copy_declarations_are_rejected_as_duplicates(self):
        self.write("ya.make", "LIBRARY()\nCOPY_FILE(value.txt copied.txt)\nCOPY_FILE(value.txt copied.txt)\nRESOURCE(copied.txt /copy)\nEND()\n")
        self.write("value.txt", "42\n")
        self.assertNotEqual(self.make().returncode, 0)
