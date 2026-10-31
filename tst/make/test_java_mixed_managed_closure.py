from tst.make.lib import MakeTest, PROJECT


class JavaMixedManagedClosureTest(MakeTest):
    def test_launcher_keeps_java_and_native_peers_in_its_closure(self):
        self.write("ya.make", f"JAVA_PROGRAM(answer)\nJDK_VERSION(17)\nJAVA_SRCS(Answer.java)\nPEERDIR({PROJECT}/peer {PROJECT}/native)\nEND()\n")
        self.write("Answer.java", "public class Answer { public static void main(String[] args) {} }\n")
        self.write("peer/ya.make", "JAVA_LIBRARY(peer)\nJDK_VERSION(17)\nJAVA_SRCS(Peer.java)\nEND()\n")
        self.write("peer/Peer.java", "public class Peer {}\n")
        self.write("native/ya.make", "DLL(native)\nSRCS(answer.cpp)\nEND()\n")
        self.write("native/answer.cpp", 'extern "C" int answer() { return 42; }\n')
        graph = self.graph()
        launcher = self.node(graph, "answer.run.cp.jar")
        for output in ("peer/peer.jar", "native/libnative.so"):
            with self.subTest(output=output):
                peer = self.node(graph, output)
                self.assertIn(f"$(B)/{PROJECT}/{output}", launcher["inputs"])
                self.assertIn(peer["uid"], launcher["deps"])
