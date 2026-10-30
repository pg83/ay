from tst.make.lib import MakeTest, PROJECT


class DescriptorPeerValidationTest(MakeTest):
    def test_descriptor_peers_require_a_module(self):
        self.write("ya.make", f"PROTO_LIBRARY(schema)\nONLY_TAGS(DESC_PROTO)\nPEERDIR({PROJECT}/missing)\nSRCS(answer.proto)\nEND()\n")
        self.write("answer.proto", 'syntax = "proto3";\nmessage Answer {}\n')
        self.assertNotEqual(self.make().returncode, 0)

    def test_documentation_peers_require_a_module(self):
        self.write("ya.make", f"PROTO_LIBRARY(schema)\nONLY_TAGS(DOCS_PROTO)\nPEERDIR({PROJECT}/missing)\nSRCS(answer.proto)\nEND()\n")
        self.write("answer.proto", 'syntax = "proto3";\nmessage Answer {}\n')
        self.assertNotEqual(self.make().returncode, 0)

    def test_documentation_peers_require_a_module_declaration(self):
        self.write("ya.make", f"PROTO_LIBRARY(schema)\nONLY_TAGS(DOCS_PROTO)\nPEERDIR({PROJECT}/moduleless)\nSRCS(answer.proto)\nEND()\n")
        self.write("answer.proto", 'syntax = "proto3";\nmessage Answer {}\n')
        self.write("moduleless/ya.make", "RECURSE()\n")
        self.assertNotEqual(self.make().returncode, 0)
