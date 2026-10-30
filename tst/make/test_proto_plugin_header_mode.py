from tst.make.lib import MakeTest, PROJECT


class ProtoPluginHeaderModeTest(MakeTest):
    def test_header_mode_assignment_preserves_declared_proto_plugins(self):
        self.write("ya.make", f"PROTO_LIBRARY()\nONLY_TAGS(CPP_PROTO)\nCPP_PROTO_PLUGIN(extra {PROJECT}/plugin .extra.h)\nSET(PROTOC_TRANSITIVE_HEADERS yes)\nSRCS(answer.proto)\nEND()\n")
        self.write("answer.proto", 'syntax = "proto3";\nmessage Answer { int32 value = 1; }\n')
        self.write("plugin/ya.make", "PY3_PROGRAM(plugin)\nPY_SRCS(MAIN main.py)\nEND()\n")
        self.write("plugin/main.py", "import sys\nsys.stdin.buffer.read()\n")
        node = self.node(self.graph(), "answer.extra.h")
        for output in ("answer.pb.h", "answer.pb.cc"):
            self.assertIn(f"$(B)/{PROJECT}/{output}", node["outputs"])
        args = [arg for command in node["cmds"] for arg in command["cmd_args"]]
        self.assertTrue(any("protoc-gen-extra=" in arg for arg in args))
