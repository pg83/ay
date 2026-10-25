from tst.make.lib import MakeTest


class ConfigurationErrorsTest(MakeTest):
    def test_invalid_descriptions_fail(self):
        descriptions = (
            "LIBRARY()\nUNKNOWN_E2E_MACRO()\nEND()\n",
            "LIBRARY()\nSRCS(answer.cpp)\n",
            "LIBRARY()\nIF(yes)\nEND()\n",
            "LIBRARY()\nINCLUDE(missing.inc)\nEND()\n",
            "LIBRARY()\nPEERDIR(__missing_e2e_peer__)\nEND()\n",
            "LIBRARY()\nCONFIGURE_FILE()\nEND()\n",
            "LIBRARY()\nRUN_PROGRAM()\nEND()\n",
            "LIBRARY()\nRUN_PYTHON3()\nEND()\n",
            "LIBRARY()\nRESOURCE(value.txt)\nEND()\n",
            "LIBRARY()\nSET(VALUE 42)\nIF(VALUE BAD_OPERATOR 17)\nENDIF()\nEND()\n",
        )
        self.write("answer.cpp", "int answer() { return 42; }\n")
        for description in descriptions:
            with self.subTest(description=description):
                self.write("ya.make", description)
                result = self.make()
                self.assertNotEqual(result.returncode, 0, result.stderr)

    def test_missing_include_fails(self):
        self.write("ya.make", "LIBRARY()\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", '#include "does_not_exist.h"\n')
        result = self.make()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("does_not_exist.h", result.stderr)
