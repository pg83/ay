from tst.make.lib import MakeTest, PROJECT


class ProtoDescriptorInputsTest(MakeTest):
    def test_gazetteer_sources_are_converted_before_descriptor_generation(self):
        self.write("ya.make", "PROTO_LIBRARY(schema)\nONLY_TAGS(DESC_PROTO)\nSRCS(answer.gztproto)\nEND()\n")
        self.write("answer.gztproto", 'syntax = "proto2";\nmessage Answer { optional int32 value = 1; }\n')
        graph = self.graph()
        converted = self.node(graph, "answer.proto")
        descriptor = self.node(graph, "answer.proto.desc")
        self.assertIn(f"$(S)/{PROJECT}/answer.gztproto", converted["inputs"])
        self.assertIn(f"$(B)/{PROJECT}/answer.proto", descriptor["inputs"])
        self.assertIn(converted["uid"], descriptor["deps"])

    def test_common_google_api_imports_are_descriptor_inputs(self):
        self.write("ya.make", "PROTO_LIBRARY(schema)\nONLY_TAGS(DESC_PROTO)\nUSE_COMMON_GOOGLE_APIS()\nPEERDIR(contrib/libs/googleapis-common-protos)\nSRCS(answer.proto)\nEND()\n")
        self.write("answer.proto", 'syntax = "proto3";\nimport "google/type/date.proto";\nmessage Answer { google.type.Date value = 1; }\n')
        node = self.node(self.graph(), "answer.proto.desc")
        self.assertIn("$(S)/contrib/libs/googleapis-common-protos/google/type/date.proto", node["inputs"])
