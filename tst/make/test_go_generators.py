from tst.make.lib import MakeTest, PROJECT


class GoGeneratorsTest(MakeTest):
    def test_mock_reflector(self):
        self.write("ya.make", f"GO_PROGRAM(reflector)\nGO_MOCKGEN_FROM({PROJECT}/source)\nGO_MOCKGEN_TYPES(Answerer)\nGO_MOCKGEN_REFLECT()\nEND()\n")
        self.write("source/ya.make", "GO_LIBRARY()\nSRCS(api.go)\nEND()\n")
        self.write("source/api.go", "package source\ntype Answerer interface { Answer() int }\n")
        self.node(self.graph(), "main.go")

    def test_mock_reflected_interfaces(self):
        self.write("ya.make", f"GO_LIBRARY()\nGO_MOCKGEN_FROM({PROJECT}/source)\nGO_MOCKGEN_PACKAGE(mocks)\nGO_MOCKGEN_TYPES(Answerer)\nGO_MOCKGEN_EXCLUDE_INTERFACES(Ignored)\nGO_MOCKGEN_MOCKS()\nEND()\n")
        self.write("gen/ya.make", f"GO_PROGRAM(reflector)\nGO_MOCKGEN_FROM({PROJECT}/source)\nGO_MOCKGEN_TYPES(Answerer)\nGO_MOCKGEN_REFLECT()\nEND()\n")
        self.write("source/ya.make", "GO_LIBRARY()\nSRCS(api.go)\nEND()\n")
        self.write("source/api.go", "package source\ntype Answerer interface { Answer() int }\ntype Ignored interface { Ignore() }\n")
        self.node(self.graph(), "main.go")

    def test_openapi_v2(self):
        self.write("ya.make", "GO_LIBRARY()\nGO_OAPI_CODEGEN_V2(types answer api.yaml IN_NOPARSE data.json)\nEND()\n")
        self.write("api.yaml", "openapi: 3.0.0\ninfo:\n  title: Answer\n  version: 1.0.0\npaths: {}\ncomponents:\n  schemas:\n    Answer:\n      type: integer\n")
        self.write("data.json", "{}\n")
        self.node(self.graph(), "types.gen.go")

    def test_gateway(self):
        self.write("ya.make", "PROTO_LIBRARY()\nONLY_TAGS(GO_PROTO)\nGRPC()\nGO_GRPC_GATEWAY_V2_SRCS(answer.proto)\nSRCS(answer.proto)\nEND()\n")
        self.write("answer.proto", f'syntax = "proto3"; package e2e; option go_package = "a.yandex-team.ru/{PROJECT}"; message Answer {{ int32 value = 1; }} service Answers {{ rpc Get(Answer) returns (Answer); }}\n')
        self.node(self.graph(), "answer.pb.gw.go")
