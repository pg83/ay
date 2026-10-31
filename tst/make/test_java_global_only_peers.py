from tst.make.lib import MakeTest, PROJECT


class JavaGlobalOnlyPeersTest(MakeTest):
    def test_java_module_keeps_a_global_only_static_peer(self):
        self.write("ya.make", f"JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nJAVA_SRCS(Answer.java)\nPEERDIR({PROJECT}/native)\nEND()\n")
        self.write("Answer.java", "public class Answer {}\n")
        self.write("native/ya.make", "LIBRARY(native)\nSRCS(GLOBAL value.cpp)\nEND()\n")
        self.write("native/value.cpp", "int value() { return 42; }\n")
        graph = self.graph()
        archive = self.node(graph, "native/libnative.global.a")
        self.assertIn(self.node(graph, "native/value.cpp.o")["uid"], archive["deps"])
        jar = self.node(graph, "answer.jar")
        self.assertIn(archive["uid"], jar["deps"])
        self.assertIn(f"$(B)/{PROJECT}/native/libnative.global.a", jar["inputs"])
