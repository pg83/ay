from tst.make.lib import MakeTest


class TypescriptPrepareSelectionTest(MakeTest):
    def test_typescript_proto_output_can_select_only_the_build_variant(self):
        self.write("ya.make", "PROTO_LIBRARY()\nONLY_TAGS(TS_PROTO)\nTS_PROTO_PACKAGE_NAME(@e2e/answer)\nSRCS(answer.proto)\nEND()\n")
        self.write("answer.proto", 'syntax = "proto3"; message Answer {}\n')
        graph = self.graph()
        self.assertIn(self.node(graph, "output.tar")["uid"], graph["result"])
