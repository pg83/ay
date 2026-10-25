from tst.make.lib import MakeTest, PROJECT


class ProtobufTest(MakeTest):
    def test_proto_import_and_generated_cpp(self):
        self.write("ya.make", "PROTO_LIBRARY()\nSRCS(base.proto message.proto)\nEND()\n")
        self.write("base.proto", 'syntax = "proto3";\npackage e2e;\nmessage Base { string value = 1; }\n')
        self.write("message.proto", f'syntax = "proto3";\npackage e2e;\nimport "{PROJECT}/base.proto";\nmessage Message {{ Base base = 1; }}\n')
        graph = self.graph()
        generator = self.node(graph, "message.pb.cc")
        self.assertIn(f"$(S)/{PROJECT}/message.proto", generator["inputs"])
        self.assertIn(f"$(S)/{PROJECT}/base.proto", generator["inputs"])
        compilation = self.node(graph, "message.pb.cc.o")
        self.assertIn(f"$(B)/{PROJECT}/message.pb.cc", compilation["inputs"])
        self.assertIn(generator["uid"], compilation["deps"])
