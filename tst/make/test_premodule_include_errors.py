from tst.make.lib import MakeTest


class PremoduleIncludeErrorsTest(MakeTest):
    def test_missing_include_before_the_module_is_rejected(self):
        self.write("ya.make", "INCLUDE(missing.inc)\nLIBRARY()\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        self.assertNotEqual(self.make().returncode, 0)
