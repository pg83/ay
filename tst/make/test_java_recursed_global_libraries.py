from tst.make.lib import MakeTest


class JavaRecursedGlobalLibrariesTest(MakeTest):
    def test_recursed_global_libraries_remain_results_with_repeated_recurses(self):
        self.write("ya.make", "JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nJAVA_SRCS(Answer.java)\nEND()\nRECURSE(native native)\n")
        self.write("Answer.java", "public class Answer {}\n")
        self.write("native/ya.make", "LIBRARY(native)\nSRCS(GLOBAL answer.cpp)\nEND()\n")
        self.write("native/answer.cpp", "int answer() { return 42; }\n")
        graph = self.graph()
        archive = self.node(graph, "native/libnative.global.a")
        self.assertIn(archive["uid"], graph["result"])
        self.assertIn(self.node(graph, "answer.jar")["uid"], graph["result"])
