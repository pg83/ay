from tst.make.lib import MakeTest, PROJECT


class ProtoVariantsTest(MakeTest):
    def test_grpc(self):
        self.write("ya.make", "PROTO_LIBRARY()\nGRPC()\nSRCS(answer.proto)\nEND()\n")
        self.write("answer.proto", 'syntax = "proto3"; package e2e; message Answer { int32 value = 1; } service Answers { rpc Get(Answer) returns (Answer); }\n')
        self.node(self.graph(), "answer.grpc.pb.cc")

    def test_go_proto(self):
        self.write("ya.make", f"GO_PROGRAM(answer)\nSRCS(main.go)\nPEERDIR({PROJECT}/proto)\nEND()\n")
        self.write("main.go", 'package main\nfunc main() {}\n')
        self.write("proto/ya.make", "PROTO_LIBRARY()\nGRPC()\nSRCS(answer.proto)\nEND()\n")
        self.write("proto/answer.proto", f'syntax = "proto3"; package e2e; option go_package = "a.yandex-team.ru/{PROJECT}/proto"; message Answer {{ int32 value = 1; }} service Answers {{ rpc Get(Answer) returns (Answer); }}\n')
        graph = self.graph()
        self.assertTrue(any(f"$(S)/{PROJECT}/proto/answer.proto" in node["inputs"] and any(path.endswith(".go") for path in node["outputs"]) for node in graph["graph"]))

    def test_proto_descriptor(self):
        self.write("ya.make", "PROTO_LIBRARY()\nONLY_TAGS(DESC_PROTO)\nSRCS(answer.proto)\nEND()\n")
        self.write("answer.proto", 'syntax = "proto3"; package e2e; message Answer { int32 value = 1; }\n')
        graph = self.graph()
        self.assertTrue(any(path.endswith(".protodesc") for node in graph["graph"] for path in node["outputs"]))

    def test_custom_namespace(self):
        self.write("ya.make", f"PROTO_LIBRARY()\nPROTO_NAMESPACE({PROJECT})\nSRCS(answer.proto)\nEND()\n")
        self.write("answer.proto", 'syntax = "proto3"; package e2e; message Answer { int32 value = 1; }\n')
        graph = self.graph()
        self.assertTrue(any(f"$(S)/{PROJECT}/answer.proto" in node["inputs"] and any(path.endswith(".pb.cc") for path in node["outputs"]) for node in graph["graph"]))
