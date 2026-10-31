from tst.make.lib import MakeTest


class ConflictingModuleDeclarationsTest(MakeTest):
    def test_an_unclosed_second_module_is_rejected(self):
        self.write("ya.make", "LIBRARY(first)\nEND()\nLIBRARY(second)\n")
        self.assertNotEqual(self.make().returncode, 0)
