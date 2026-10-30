import json

from tst.make.lib import MakeTest


class CompilerExpressionsTest(MakeTest):
    def flags(self, value):
        config = self.root / "ya.conf"
        self.write_tree("ya.conf", config.read_text() + "\n[flags]\nCFLAGS = " + json.dumps(value) + "\n")
        self.write("ya.make", "LIBRARY()\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")

    def test_trailing_command_separator_in_configured_flags(self):
        self.flags("-DE2E=42 && &&")
        node = self.node(self.graph(), "answer.cpp.o")
        args = [arg for command in node["cmds"] for arg in command["cmd_args"]]
        self.assertIn("-DE2E=42", args)
        self.assertNotIn("&&", args)

    def test_literal_modifiers_in_configured_flags(self):
        self.flags('-DE2E_QUOTED=${quo:"a b"} -DE2E_COMMA=${comma:"ignored"} -DE2E_LITERAL=${"text"}')
        node = self.node(self.graph(), "answer.cpp.o")
        args = [arg for command in node["cmds"] for arg in command["cmd_args"]]
        for arg in ("-DE2E_QUOTED=a b", "-DE2E_COMMA=,", "-DE2E_LITERAL=text"):
            self.assertIn(arg, args)

    def test_unterminated_quotes_in_configured_flags_are_errors(self):
        self.flags('-DE2E="unterminated')
        self.assertNotEqual(self.make().returncode, 0)

    def test_attached_command_separator_in_module_flags(self):
        self.write("ya.make", "LIBRARY()\nCFLAGS(-DE2E=42&&)\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        args = [arg for command in self.node(self.graph(), "answer.cpp.o")["cmds"] for arg in command["cmd_args"]]
        self.assertIn("-DE2E=42", args)
        self.assertFalse(any("&&" in arg for arg in args))
