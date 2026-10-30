from tst.make.lib import MakeTest


class CythonGeneratorInputValidationTest(MakeTest):
    def test_missing_cimport_dependencies_of_generator_inputs_are_rejected(self):
        self.write("ya.make", "LIBRARY()\nRUN_PYTHON3(generate.py IN answer.pyx STDOUT generated.cpp)\nEND()\n")
        self.write("generate.py", 'print("int answer() { return 42; }")\n')
        self.write("answer.pyx", "from e2e_missing cimport Answer\n")
        self.assertNotEqual(self.make().returncode, 0)
