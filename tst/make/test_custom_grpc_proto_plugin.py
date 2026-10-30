from tst.make.lib import MakeTest, PROJECT


class CustomGrpcProtoPluginTest(MakeTest):
    def test_custom_grpc_outputs_keep_imported_header_dependencies(self):
        self.write("ya.make", "PROTO_LIBRARY()\nCPP_PROTO_PLUGIN2(grpc contrib/tools/protoc/plugins/grpc_cpp .grpc.pb.h .grpc.pb.cc DEPS contrib/libs/grpc)\nSRCS(base.proto answer.proto)\nEND()\n")
        self.write("base.proto", 'syntax = "proto3";\npackage e2e;\nmessage Base { int32 value = 1; }\n')
        self.write("answer.proto", f'syntax = "proto3";\npackage e2e;\nimport "{PROJECT}/base.proto";\nservice Answer {{ rpc Get(Base) returns (Base); }}\n')
        graph = self.graph()
        generated = self.node(graph, "answer.grpc.pb.cc")
        self.assertIn(f"$(B)/{PROJECT}/answer.grpc.pb.h", generated["outputs"])
        self.assertIn(f"$(S)/{PROJECT}/base.proto", generated["inputs"])
        compiled = self.node(graph, "answer.grpc.pb.cc.o")
        self.assertIn(f"$(B)/{PROJECT}/base.pb.h", compiled["inputs"])
        self.assertIn(generated["uid"], compiled["deps"])
