from tst.make.lib import MakeTest


class PythonGeneratorLiteralDollarTest(MakeTest):
    def test_standalone_dollar_arguments_reach_python_generators(self):
        self.write("ya.make", "LIBRARY()\nRUN_PYTHON3(generate.py $ STDOUT generated.cpp)\nEND()\n")
        self.write("generate.py", "print('int answer() { return 42; }')\n")
        generated = self.node(self.graph(), "generated.cpp")
        args = [arg for command in generated["cmds"] for arg in command["cmd_args"]]
        self.assertIn("$", args)
