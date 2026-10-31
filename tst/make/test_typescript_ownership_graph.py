from tst.make.lib import MakeTest


class TypescriptOwnershipGraphTest(MakeTest):
    def test_typescript_proto_graph_is_available_with_ownership_diagnostics(self):
        self.write("ya.make", "PROTO_LIBRARY()\nONLY_TAGS(TS_PROTO TS_PREPARE_DEPS)\nTS_PROTO_PACKAGE_NAME(@e2e/answer)\nSRCS(answer.proto)\nEND()\n")
        self.write("answer.proto", 'syntax = "proto3"; message Answer {}\n')
        graph = self.graph(env={"AY_DEBUG_OWNERSHIP": "1"})
        self.assertIn(self.node(graph, "output.tar")["uid"], graph["result"])
