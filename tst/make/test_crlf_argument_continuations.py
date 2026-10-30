from tst.make.lib import MakeTest


class CrlfArgumentContinuationsTest(MakeTest):
    def test_compiler_flags_preserve_carriage_return_from_a_crlf_continuation(self):
        self.write("ya.make", "LIBRARY()\nCFLAGS(-DE2E_FIRST=1\\\r\n    -DE2E_SECOND=2)\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        compiled = self.node(self.graph(), "answer.cpp.o")
        args = [arg for command in compiled["cmds"] for arg in command["cmd_args"]]
        self.assertIn("-DE2E_FIRST=1\r", args)
        self.assertIn("-DE2E_SECOND=2", args)
