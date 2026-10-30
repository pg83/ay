from tst.make.lib import MakeTest, PROJECT


class UnconfiguredPluginToolsTest(MakeTest):
    def test_proto_plugin_requires_a_module_declaration(self):
        self.write("plugin/ya.make", "RECURSE(child)\n")
        self.write("plugin/child/ya.make", "LIBRARY()\nEND()\n")
        self.write("ya.make", f"PROTO_LIBRARY()\nCPP_PROTO_PLUGIN(extra {PROJECT}/plugin .extra.h)\nSRCS(answer.proto)\nEND()\n")
        self.write("answer.proto", 'syntax = "proto2"; package e2e; message Answer {}\n')
        self.assertNotEqual(self.make().returncode, 0)
