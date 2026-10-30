from tst.make.lib import MakeTest


class IncompleteConditionSyntaxTest(MakeTest):
    def assert_condition_is_false(self, condition):
        self.write("ya.make", f"LIBRARY()\nSET(E2E_VALUE yes)\nIF({condition})\nCFLAGS(-DE2E_UNEXPECTED)\nENDIF()\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        node = self.node(self.graph(), "answer.cpp.o")
        args = [arg for command in node["cmds"] for arg in command["cmd_args"]]
        self.assertNotIn("-DE2E_UNEXPECTED", args)

    def test_missing_comparison_operand_is_false(self):
        self.assert_condition_is_false("E2E_VALUE ==")

    def test_trailing_comparison_tokens_are_false(self):
        self.assert_condition_is_false("E2E_VALUE == yes trailing")

    def test_configuration_comparison_requires_a_right_operand(self):
        core = self.root / "build/ymake.core.conf"
        self.write_tree("build/ymake.core.conf", core.read_text() + '\nmacro E2E_INVALID_CONDITION() {\n    when ($E2E_VALUE ==) {\n        CFLAGS(-DE2E_UNEXPECTED)\n    }\n}\n')
        self.write("ya.make", "LIBRARY()\nE2E_INVALID_CONDITION()\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        self.assertNotEqual(self.make().returncode, 0)
