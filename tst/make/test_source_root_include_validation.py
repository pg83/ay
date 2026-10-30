from tst.make.lib import MakeTest


class SourceRootIncludeValidationTest(MakeTest):
    def test_an_include_cannot_name_the_source_root_directory(self):
        self.write("ya.make", "LIBRARY()\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", '#include ".."\nint answer() { return 42; }\n')
        self.assertNotEqual(self.make().returncode, 0)
