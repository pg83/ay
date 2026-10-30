from tst.make.lib import MakeTest


class ConditionBoundariesTest(MakeTest):
    def fixture(self, condition, version="2.10.1"):
        self.write("ya.make", f"LIBRARY()\nSET(E2E_VALUE invalid)\nSET(E2E_VERSION {version})\nIF({condition})\nCFLAGS(-DE2E_SELECTED=yes)\nELSE()\nCFLAGS(-DE2E_SELECTED=no)\nENDIF()\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")

    def selected(self):
        node = self.node(self.graph(), "answer.cpp.o")
        return [arg for command in node["cmds"] for arg in command["cmd_args"]]

    def test_version_greater_than_compares_components(self):
        self.fixture("E2E_VERSION VERSION_GT 2.9.9")
        self.assertIn("-DE2E_SELECTED=yes", self.selected())

    def test_false_numeric_predicate_can_be_a_comparison_operand(self):
        self.fixture("ISNUM E2E_VALUE == no")
        self.assertIn("-DE2E_SELECTED=yes", self.selected())

    def test_non_numeric_values_compare_as_zero(self):
        self.fixture("E2E_VALUE < 1")
        self.assertIn("-DE2E_SELECTED=yes", self.selected())

    def test_malformed_version_components_are_rejected(self):
        self.fixture("E2E_VERSION VERSION_GT 1.0.0", version="bad.1.0")
        self.assertNotEqual(self.make().returncode, 0)
