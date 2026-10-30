from tst.make.lib import MakeTest, PROJECT


class ProtoCommentKeywordsTest(MakeTest):
    def test_bare_import_in_a_comment_does_not_add_a_dependency(self):
        self.write("ya.make", "PROTO_LIBRARY()\nSRCS(answer.proto)\nEND()\n")
        self.write("answer.proto", 'syntax = "proto2";\n/*\nimport\n*/\nmessage Answer {}\n')
        generated = self.node(self.graph(), "answer.pb.h")
        self.assertIn(f"$(S)/{PROJECT}/answer.proto", generated["inputs"])
        self.assertFalse(any(path.endswith("/import") for path in generated["inputs"]))
