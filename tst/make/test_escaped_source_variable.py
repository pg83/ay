from tst.make.lib import MakeTest


class EscapedSourceVariableTest(MakeTest):
    def test_an_escaped_reference_does_not_substitute_a_source_variable(self):
        self.write("ya.make", "LIBRARY()\nSET(E2E_SOURCE answer.cpp)\nSRCS(\\${E2E_SOURCE})\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        result = self.make()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("${E2E_SOURCE}", result.stderr)
