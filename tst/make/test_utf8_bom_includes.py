from tst.make.lib import MakeTest


class Utf8BomIncludesTest(MakeTest):
    def test_proto_imports_after_a_utf8_bom_remain_generator_inputs(self):
        self.write("ya.make", "PROTO_LIBRARY()\nONLY_TAGS(CPP_PROTO)\nSRCS(answer.proto value.proto)\nEND()\n")
        self.write("answer.proto", '\ufeffsyntax = "proto3";\nimport "value.proto";\nmessage Answer { Value value = 1; }\n')
        self.write("value.proto", 'syntax = "proto3";\nmessage Value { int32 number = 1; }\n')
        generated = self.node(self.graph(), "answer.pb.cc")
        self.assertIn(f"$(S)/{self.project_name}/value.proto", generated["inputs"])
