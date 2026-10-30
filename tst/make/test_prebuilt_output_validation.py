from tst.make.lib import MakeTest


class PrebuiltOutputValidationTest(MakeTest):
    def test_recursive_primary_output_expansions_are_rejected(self):
        self.write("ya.make", "PREBUILT_PROGRAM(answer)\nPRIMARY_OUTPUT(${E2E_OUTPUT})\nEND()\n")
        result = self.make("-DE2E_OUTPUT=$E2E_OUTPUT/next")
        self.assertNotEqual(result.returncode, 0)
