from tst.make.lib import MakeTest


class JavaSourceRootGlobTest(MakeTest):
    def test_root_globs_normalize_their_fixed_directory_component(self):
        self.write("ya.make", "JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nJAVA_SRCS(Main.java)\nJAVA_SRCS(SRCDIR ${ARCADIA_ROOT}/./ *.e2e_unmatched)\nEND()\n")
        self.write("Main.java", "public class Main {}\n")
        node = self.node(self.graph(), "answer.jar")
        self.assertIn(f"$(S)/{self.project_name}/Main.java", node["inputs"])
        self.assertNotIn("$(S)", node["inputs"])
