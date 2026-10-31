from tst.make.lib import MakeTest


class UnbracedGeneratorToolsTest(MakeTest):
    def test_empty_unbraced_tool_variables_are_configuration_errors(self):
        self.write("ya.make", "LIBRARY()\nSET(E2E_TOOL)\nRUN_PROGRAM($E2E_TOOL OUT generated.cpp)\nEND()\n")
        self.assertNotEqual(self.make().returncode, 0)
