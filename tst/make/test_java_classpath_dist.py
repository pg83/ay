from tst.make.lib import MakeTest


class JavaClasspathDistTest(MakeTest):
    def test_distributed_classpath_checks_request_verbose_diagnostics(self):
        self.write("ya.make", "JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nJAVA_SRCS(Answer.java)\nCHECK_JAVA_DEPS(yes)\nEND()\n")
        self.write("Answer.java", "public class Answer {}\n")
        graph = self.graph("-t", "--dist")
        checks = [command["cmd_args"] for node in graph["graph"] for command in node["cmds"] if "run_classpath_clash" in command["cmd_args"]]
        self.assertEqual(len(checks), 1)
        self.assertIn("--verbose", checks[0])
