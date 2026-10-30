from tst.make.lib import MakeTest


class PythonVariantMacroRestrictionsTest(MakeTest):
    def test_python2_extensions_reject_the_python3_module_selector(self):
        self.write("ya.make", "PY2MODULE(answer)\nPYTHON3_MODULE()\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        self.assertNotEqual(self.make().returncode, 0)
