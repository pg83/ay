from tst.make.lib import MakeTest, PROJECT


class IncludeLanguageSelectionTest(MakeTest):
    def test_unknown_include_languages_are_rejected(self):
        self.write("answer.cpp", "int answer() { return 42; }\n")
        self.write("include/value.h", "#pragma once\n")
        for scope in ("", "GLOBAL "):
            with self.subTest(scope=scope):
                self.write("ya.make", f"LIBRARY()\nADDINCL({scope}FOR e2e {PROJECT}/include)\nSRCS(answer.cpp)\nEND()\n")
                self.assertNotEqual(self.make().returncode, 0)
