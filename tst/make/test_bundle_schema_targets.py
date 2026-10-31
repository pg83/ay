from tst.make.lib import MakeTest, PROJECT


class BundleSchemaTargetsTest(MakeTest):
    def test_schema_bundle_consumes_the_merged_descriptor_artifact(self):
        self.write("ya.make", f"LIBRARY(answer)\nBUNDLE({PROJECT}/schema NAME schema.bundle)\nRESOURCE(schema.bundle /schema)\nEND()\n")
        self.write("schema/ya.make", "PROTO_SCHEMA(schema)\nSRCS(answer.proto)\nEND()\n")
        self.write("schema/answer.proto", 'syntax = "proto3"; message Answer {}\n')
        graph = self.graph()
        descriptor = self.node(graph, "schema/schema.protodesc")
        bundled = self.node(graph, "schema.bundle")
        self.assertIn(f"$(B)/{PROJECT}/schema/schema.protodesc", bundled["inputs"])
        self.assertIn(descriptor["uid"], bundled["deps"])
