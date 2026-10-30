from tst.make.lib import MakeTest, PROJECT


class GazetteerImportContextsTest(MakeTest):
    def test_imported_gazetteers_reach_conversion_and_proto_compilation(self):
        self.write("ya.make", "LIBRARY()\nADDINCL(${ARCADIA_ROOT} ${ARCADIA_BUILD_ROOT})\nSRCS(answer.gztproto dependency.gztproto)\nEND()\n")
        self.write("answer.gztproto", 'import "dependency.gztproto";\npackage e2e; message Answer { optional Dependency value = 1; }\n')
        self.write("dependency.gztproto", "package e2e; message Dependency {}\n")
        graph = self.graph()
        converted = self.node(graph, "answer.proto")
        compiled = self.node(graph, "answer.pb.cc")
        dependency = self.node(graph, "dependency.proto")
        self.assertIn(f"$(S)/{PROJECT}/dependency.gztproto", converted["inputs"])
        self.assertIn(f"$(B)/{PROJECT}/dependency.proto", compiled["inputs"])
        self.assertIn(dependency["uid"], compiled["deps"])

    def test_python_only_gazetteers_require_an_intermediate_proto(self):
        self.write("ya.make", "PROTO_LIBRARY()\nONLY_TAGS(PY3_PROTO)\nSRCS(answer.gztproto)\nEND()\n")
        self.write("answer.gztproto", "package e2e; message Answer {}\n")
        result = self.make()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("answer.proto", result.stderr)
