from tst.make.lib import MakeTest, PROJECT


class JavaDescriptorPeersTest(MakeTest):
    def test_java_module_rejects_a_descriptor_collection_peer(self):
        self.write("ya.make", f"JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nJAVA_SRCS(Answer.java)\nPEERDIR({PROJECT}/descriptors)\nEND()\n")
        self.write("Answer.java", "public class Answer {}\n")
        self.write("descriptors/ya.make", f"PROTO_DESCRIPTIONS(descriptors)\nPEERDIR({PROJECT}/schema)\nEND()\n")
        self.write("schema/ya.make", "PROTO_LIBRARY(schema)\nSRCS(answer.proto)\nEND()\n")
        self.write("schema/answer.proto", 'syntax = "proto3"; message Answer {}\n')
        self.assertNotEqual(self.make().returncode, 0)
