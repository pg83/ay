from tst.make.lib import MakeTest


class ShorthandSourceRootsTest(MakeTest):
    def test_root_shorthand_source_directories_are_rejected(self):
        self.write("ya.make", "LIBRARY(answer)\nSRCDIR($S $B)\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        self.assertNotEqual(self.make().returncode, 0)
