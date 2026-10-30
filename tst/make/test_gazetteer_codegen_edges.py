from tst.make.lib import MakeTest, PROJECT


class GazetteerCodegenEdgesTest(MakeTest):
    def test_root_proto_namespace_preserves_the_gazetteer_source(self):
        self.write("ya.make", "PROTO_LIBRARY()\nONLY_TAGS(CPP_PROTO)\nPROTO_NAMESPACE(GLOBAL .)\nSRCS(answer.gztproto)\nEND()\n")
        self.write("answer.gztproto", 'syntax = "proto2";\nmessage Answer { optional int32 value = 1; }\n')
        node = self.node(self.graph(), "answer.proto")
        self.assertIn(f"$(S)/{PROJECT}/answer.gztproto", node["inputs"])

    def test_repeated_gazetteer_sources_are_rejected(self):
        self.write("ya.make", "LIBRARY()\nSRCS(answer.gztproto answer.gztproto)\nEND()\n")
        self.write("answer.gztproto", 'syntax = "proto2";\nmessage Answer {}\n')
        self.assertNotEqual(self.make().returncode, 0)
