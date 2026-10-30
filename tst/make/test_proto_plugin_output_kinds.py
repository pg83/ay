from tst.make.lib import MakeTest, PROJECT


class ProtoPluginOutputKindsTest(MakeTest):
    def plugin(self):
        self.write("plugin/ya.make", "PY3_PROGRAM(plugin)\nPY_SRCS(MAIN main.py)\nEND()\n")
        self.write("plugin/main.py", "import sys\nsys.stdin.buffer.read()\n")

    def test_binary_plugin_outputs_are_preserved(self):
        self.plugin()
        self.write("ya.make", f"PROTO_LIBRARY()\nCPP_PROTO_PLUGIN(extra {PROJECT}/plugin .extra.bin)\nSRCS(answer.proto)\nEND()\n")
        self.write("answer.proto", 'syntax = "proto3";\nmessage Answer { int32 value = 1; }\n')
        node = self.node(self.graph(), "answer.extra.bin")
        self.assertIn(f"$(S)/{PROJECT}/answer.proto", node["inputs"])

    def test_regular_proto_plugins_do_not_apply_to_events(self):
        self.plugin()
        self.write("ya.make", f"LIBRARY()\nCPP_PROTO_PLUGIN(extra {PROJECT}/plugin .extra.h)\nSRCS(answer.ev)\nEND()\n")
        self.write("answer.ev", 'syntax = "proto2";\nmessage Answer { optional int32 value = 1; }\n')
        graph = self.graph()
        generated = self.node(graph, "answer.ev.pb.cc")
        self.assertFalse(any(out.endswith(".extra.h") for node in graph["graph"] for out in node["outputs"]))
        self.assertFalse(any("protoc-gen-extra" in arg for command in generated["cmds"] for arg in command["cmd_args"]))
