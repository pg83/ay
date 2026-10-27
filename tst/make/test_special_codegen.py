from tst.make.lib import MakeTest, PROJECT


class SpecialCodegenTest(MakeTest):
    def test_go_mockgen_source(self):
        self.write("ya.make", f"GO_LIBRARY()\nGO_MOCKGEN_FROM({PROJECT}/source)\nGO_MOCKGEN_SOURCE(api.go)\nEND()\n")
        self.write("source/ya.make", "GO_LIBRARY()\nSRCS(api.go)\nEND()\n")
        self.write("source/api.go", "package source\ntype API interface { Answer() int }\n")
        self.node(self.graph(), "api.source.gen.go")

    def test_go_openapi(self):
        self.write("ya.make", "GO_LIBRARY()\nGO_OAPI_CODEGEN(types answer api.yaml)\nEND()\n")
        self.write("api.yaml", "openapi: 3.0.0\ninfo:\n  title: Answer\n  version: 1.0.0\npaths: {}\ncomponents:\n  schemas:\n    Answer:\n      type: integer\n")
        self.node(self.graph(), "types.gen.go")

    def test_ros_messages(self):
        self.write("ya.make", "ROS_LIBRARY(e2e)\nONLY_TAGS(CPP_ROS)\nSRCS(Answer.msg)\nEND()\n")
        self.write("Answer.msg", "int32 value\n")
        graph = self.graph()
        self.assertTrue(any(f"$(S)/{PROJECT}/Answer.msg" in node["inputs"] and any(path.endswith("Answer.h") for path in node["outputs"]) for node in graph["graph"]))

    def test_sdc_resource_metadata(self):
        self.write("ya.make", f"PROGRAM(answer)\nUSE_SDC_META()\nPEERDIR({PROJECT}/meta)\nSRCS(main.cpp)\nEND()\n")
        self.write("meta/ya.make", "LIBRARY()\nSDC_RESOURCE_INFO(e2e value https://example.com/value)\nEND()\n")
        self.write("main.cpp", "int main() { return 0; }\n")
        graph = self.graph()
        self.node(graph, "meta/value.sdcmeta")
        self.node(graph, "answer.sdcmeta")

    def test_proto_structs_read_source_over_copy(self):
        self.write("ya.make", f"LIBRARY()\nPROTO_NAMESPACE({PROJECT}/lib/src)\nPEERDIR({PROJECT}/lib {PROJECT}/schema)\nUSRV_GEN_PROTO_STRUCTS(PROTODESC {PROJECT}/descriptions FILE {PROJECT}/lib/src/answer.proto)\nEND()\n")
        self.write("lib/ya.make", "LIBRARY()\nFILES(src/answer.proto)\nSRCS(lib.cpp)\nEND()\n")
        self.write("lib/lib.cpp", "int lib() { return 42; }\n")
        self.write("lib/src/answer.proto", 'syntax = "proto3"; package e2e; message Answer { int32 value = 1; }\n')
        self.write("schema/ya.make", f"PROTO_LIBRARY()\nPROTO_NAMESPACE(GLOBAL {PROJECT}/lib/src)\nSRCDIR({PROJECT}/lib/src)\nSRCS(answer.proto)\nEND()\n")
        self.write("descriptions/ya.make", f"PROTO_DESCRIPTIONS()\nPEERDIR({PROJECT}/schema)\nEND()\n")
        graph = self.graph()
        structs = self.node(graph, "lib/src/answer.structs.usrv.pb.hpp")
        self.assertIn(f"$(S)/{PROJECT}/lib/src/answer.proto", structs["inputs"])
        self.assertFalse(any(f"$(B)/{PROJECT}/lib/src/answer.proto" in node["outputs"] for node in graph["graph"]))
