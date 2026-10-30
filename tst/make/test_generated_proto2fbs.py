from tst.make.lib import MakeTest, PROJECT


class GeneratedProto2FbsTest(MakeTest):
    def test_generated_protos_keep_their_producer_during_conversion(self):
        self.write("ya.make", "LIBRARY()\nCOPY_FILE(template.proto generated.proto)\nPROTO2FBS(generated.proto)\nEND()\n")
        self.write("template.proto", 'syntax = "proto2";\nmessage Answer { optional int32 value = 1; }\n')
        graph = self.graph()
        copied = self.node(graph, "generated.proto")
        converted = self.node(graph, "generated.fbs")
        self.assertIn(f"$(B)/{PROJECT}/generated.proto", converted["inputs"])
        self.assertIn(copied["uid"], converted["deps"])
