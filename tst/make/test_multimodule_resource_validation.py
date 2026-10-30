from tst.make.lib import MakeTest


class MultimoduleResourceValidationTest(MakeTest):
    def proto(self):
        self.write("answer.proto", 'syntax = "proto2"; message Answer {}\n')

    def validation_args(self, graph, name):
        node = self.node(graph, f"test-results/{name}/meta.json")
        return [arg for command in node["cmds"] for arg in command["cmd_args"]]

    def test_shared_sandbox_resource_has_one_validation_suite(self):
        self.proto()
        self.write("ya.make", "PROTO_LIBRARY()\nONLY_TAGS(CPP_PROTO PY3_PROTO)\nFROM_SANDBOX(FILE 123456789 OUT_NOAUTO value.txt)\nRESOURCE(value.txt /value)\nSRCS(answer.proto)\nEND()\n")
        args = self.validation_args(self.graph("-t"), "validate_resource")
        self.assertIn("123456789", args)

