from tst.make.lib import MakeTest


class DollarParenthesesTest(MakeTest):
    def test_empty_parenthesized_reference_is_rejected(self):
        self.write("ya.make", "LIBRARY()\nCFLAGS($())\nEND()\n")
        self.assertNotEqual(self.make().returncode, 0)

    def test_unterminated_parenthesized_reference_is_rejected(self):
        self.write("ya.make", "LIBRARY()\nCFLAGS($(unfinished")
        self.assertNotEqual(self.make().returncode, 0)

    def test_escaped_suffix_after_parenthesized_reference_is_preserved(self):
        self.write("ya.make", "LIBRARY()\nCFLAGS($(E2E)\\suffix)\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        node = self.node(self.graph(), "answer.cpp.o")
        args = [arg for command in node["cmds"] for arg in command["cmd_args"]]
        self.assertTrue(any("$(E2E)" in arg and "suffix" in arg for arg in args), args)
