from tst.make.lib import MakeTest


class ProtoAuxiliarySourcesTest(MakeTest):
    def test_proto_documentation_ignores_non_proto_headers(self):
        self.write("ya.make", "PROTO_LIBRARY()\nONLY_TAGS(DOCS_PROTO)\nSRCS(answer.proto helper.h)\nEND()\n")
        self.write("answer.proto", 'syntax = "proto3"; message Answer { int32 value = 1; }\n')
        self.write("helper.h", "#pragma once\n")
        documentation = self.node(self.graph(), "answer.md")
        self.assertIn(f"$(S)/{self.project_name}/answer.proto", documentation["inputs"])
        self.assertNotIn(f"$(S)/{self.project_name}/helper.h", documentation["inputs"])
