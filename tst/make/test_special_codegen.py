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
