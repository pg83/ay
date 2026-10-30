import json

from tst.make.lib import MakeTest


class YaConfFlagsTest(MakeTest):
    def root_flags(self, typed=False):
        config = self.root / "ya.conf"
        values = {"E2E_STRING": "root", "E2E_INTEGER": 42, "E2E_FLOAT": 1.25, "E2E_BOOLEAN": True}
        if not typed:
            values = {key: ("yes" if value is True else str(value)) for key, value in values.items()}
        text = "\n[flags]\n" + "\n".join(key + " = " + json.dumps(value) for key, value in values.items()) + "\n"
        self.write_tree("ya.conf", config.read_text() + text)
        self.write("ya.make", "LIBRARY()\nCFLAGS(-DE2E_STRING=${E2E_STRING} -DE2E_INTEGER=${E2E_INTEGER} -DE2E_FLOAT=${E2E_FLOAT})\nIF(E2E_BOOLEAN)\nCFLAGS(-DE2E_BOOLEAN_ENABLED)\nENDIF()\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")

    def test_string_flag_values_from_root_configuration(self):
        self.root_flags()
        node = self.node(self.graph(), "answer.cpp.o")
        args = [arg for command in node["cmds"] for arg in command["cmd_args"]]
        for arg in ("-DE2E_STRING=root", "-DE2E_INTEGER=42", "-DE2E_FLOAT=1.25", "-DE2E_BOOLEAN_ENABLED"):
            self.assertIn(arg, args)

    def test_non_string_configuration_flags_are_rejected(self):
        self.root_flags(typed=True)
        self.assertNotEqual(self.make().returncode, 0)

    def test_internal_configuration_does_not_override_root_flags(self):
        self.root_flags()
        self.write_tree("build/internal/ya.conf", '[flags]\nE2E_STRING = "internal"\n')
        node = self.node(self.graph(), "answer.cpp.o")
        args = [arg for command in node["cmds"] for arg in command["cmd_args"]]
        self.assertIn("-DE2E_STRING=root", args)
        self.assertNotIn("-DE2E_STRING=internal", args)
