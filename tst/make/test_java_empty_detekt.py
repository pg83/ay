from tst.make.lib import MakeTest


class JavaEmptyDetektTest(MakeTest):
    def check_no_detekt_suite(self, collect):
        self.write("ya.make", f"JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nWITH_KOTLIN()\nJAVA_SRCS(Answer.java)\n{collect}\nSTYLE_DETEKT()\nEND()\n")
        self.write("Answer.java", "public class Answer {}\n")
        self.write("empty/.keep", "")
        graph = self.graph("-t")
        self.node(graph, "answer.jar")
        commands = [command["cmd_args"] for node in graph["graph"] for command in node["cmds"]]
        self.assertFalse(any("run_custom_lint" in args and "detekt" in args for args in commands))

    def test_detekt_without_source_collection_has_no_suite(self):
        self.check_no_detekt_suite("")

    def test_detekt_with_an_empty_source_directory_has_no_suite(self):
        self.check_no_detekt_suite("COLLECT_KOTLIN_LINT_SRCS(DIRS empty)")
