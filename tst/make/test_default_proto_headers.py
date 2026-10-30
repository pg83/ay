from tst.make.lib import MakeTest, PROJECT


class DefaultProtoHeadersTest(MakeTest):
    def test_default_transitive_headers_keep_the_normal_cpp_generator(self):
        self.write("ya.make", "LIBRARY()\nDEFAULT(PROTOC_TRANSITIVE_HEADERS yes)\nSRCS(answer.proto)\nEND()\n")
        self.write("answer.proto", 'syntax = "proto3";\nmessage Answer { int32 value = 1; }\n')
        node = self.node(self.graph(), "answer.pb.cc")
        self.assertIn(f"$(S)/{PROJECT}/answer.proto", node["inputs"])
        args = [arg for command in node["cmds"] for arg in command["cmd_args"]]
        self.assertFalse(any("lite_headers" in arg for arg in args))
