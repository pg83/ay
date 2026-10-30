from tst.make.lib import MakeTest, PROJECT


class ProtoPluginToolsTest(MakeTest):
    def test_resource_only_plugin_tool_is_rejected(self):
        self.write("plugin/ya.make", "RESOURCES_LIBRARY()\nEND()\n")
        self.write("ya.make", f"PROTO_LIBRARY()\nCPP_PROTO_PLUGIN(extra {PROJECT}/plugin .extra.h)\nSRCS(answer.proto)\nEND()\n")
        self.write("answer.proto", 'syntax = "proto2"; package e2e; message Answer {}\n')
        self.assertNotEqual(self.make().returncode, 0)
