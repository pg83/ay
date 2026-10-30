from tst.make.lib import MakeTest


class LexicalConditionBoundariesTest(MakeTest):
    def test_comment_can_follow_the_opening_macro_parenthesis(self):
        self.write("ya.make", "LIBRARY(# module name follows the comment\nanswer)\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        self.node(self.graph(), "libanswer.a")

    def test_dollar_parenthesis_reference_cannot_contain_whitespace(self):
        self.write("ya.make", "LIBRARY()\nCFLAGS($(E2E_VALUE EXTRA))\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        self.assertNotEqual(self.make().returncode, 0)

    def test_nested_configuration_condition_rejects_trailing_tokens(self):
        core = self.root / "build/ymake.core.conf"
        self.write_tree("build/ymake.core.conf", core.read_text() + '\nwhen (($OS_LINUX == "yes" trailing)) {\n    E2E_INVALID = yes\n}\nmacro E2E_INVALID_CONDITION() {\n    CFLAGS(-DE2E_INVALID=$E2E_INVALID)\n}\n')
        self.write("ya.make", "LIBRARY()\nE2E_INVALID_CONDITION()\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        self.assertNotEqual(self.make().returncode, 0)
