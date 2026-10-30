from tst.make.lib import MakeTest


class EmptyIncludeScopesTest(MakeTest):
    def test_global_scope_without_paths_is_rejected(self):
        self.write("ya.make", "LIBRARY()\nADDINCL(GLOBAL)\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        self.assertNotEqual(self.make().returncode, 0)
