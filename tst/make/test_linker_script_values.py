from tst.make.lib import MakeTest


class LinkerScriptValuesTest(MakeTest):
    def test_command_line_linker_script_template_keeps_literal_arguments(self):
        self.write("ya.make", "PROGRAM(answer)\nSRCS(main.cpp)\nEND()\n")
        self.write("main.cpp", "int main() { return 0; }\n")
        linked = self.node(self.graph("-DLINKER_SCRIPT_VALUE=-Wl,--as-needed"), "answer")
        args = [arg for command in linked["cmds"] for arg in command["cmd_args"]]
        self.assertIn("-Wl,--as-needed", args)
