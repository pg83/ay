from tst.make.lib import MakeTest


class CachedSchemaInputParsersTest(MakeTest):
    def fixture(self, extension):
        self.write("ya.make", f"LIBRARY()\nADDINCL({self.project_name})\nRUN_PYTHON3(generate.py IN answer.{extension} OUT_NOAUTO value.txt)\nRESOURCE(value.txt /value)\nEND()\n")
        self.write("generate.py", "from pathlib import Path\nPath('value.txt').write_text('42')\n")
        self.write("answer." + extension, 'syntax = "proto2";\nimport "opaque.e2e";\nmessage Answer {}\n')
        self.write("opaque.e2e", 'import "cycle.e2e";\n')
        self.write("cycle.e2e", 'import "opaque.e2e";\nimport "leaf.proto";\n')
        self.write("leaf.proto", 'syntax = "proto2";\nmessage Leaf {}\n')
        node = self.node(self.graph(), "value.txt")
        for name in ("answer." + extension, "opaque.e2e", "cycle.e2e", "leaf.proto"):
            self.assertIn(f"$(S)/{self.project_name}/{name}", node["inputs"])

    def test_cyclic_opaque_proto_inputs_keep_imported_dependencies(self):
        self.fixture("proto")

    def test_cyclic_opaque_configuration_proto_inputs_keep_imported_dependencies(self):
        self.fixture("cfgproto")
