from tst.make.lib import MakeTest, PROJECT


class JavaProtoJavaPeerTest(MakeTest):
    def test_java_proto_peer_uses_its_java_library_interface(self):
        self.write("ya.make", f"JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nJAVA_SRCS(Answer.java)\nPEERDIR({PROJECT}/schema)\nEND()\n")
        self.write("Answer.java", "public class Answer {}\n")
        self.write("schema/ya.make", f"PROTO_LIBRARY(schema)\nSRCS(answer.proto)\nPEERDIR({PROJECT}/peer)\nEND()\n")
        self.write("schema/answer.proto", 'syntax = "proto3"; message Answer {}\n')
        self.write("peer/ya.make", "JAVA_LIBRARY(peer)\nJDK_VERSION(17)\nJAVA_SRCS(Peer.java)\nEND()\n")
        self.write("peer/Peer.java", "public class Peer {}\n")
        graph = self.graph()
        root = self.node(graph, "answer.jar")
        schema = self.node(graph, "schema/schema.jar")
        peer = self.node(graph, "peer/peer-interface.jar")
        self.assertIn(schema["uid"], root["deps"])
        self.assertIn(peer["uid"], schema["deps"])
        self.assertIn(f"$(B)/{PROJECT}/peer/peer-interface.jar", schema["inputs"])
