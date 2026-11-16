from tst.make.lib import MakeTest


class AutocheckConfigureErrorsTest(MakeTest):
    def test_unknown_macro(self):
        self.write("ya.make", "LIBRARY()\nUNKNOWN_E2E_MACRO()\nEND()\n")
        self.assertConfigureLikeYa()

    def test_missing_peerdir(self):
        self.write("ya.make", "LIBRARY()\nPEERDIR(__missing_e2e_peer__)\nEND()\n")
        self.assertConfigureLikeYa()

    def test_missing_source(self):
        self.write("ya.make", "LIBRARY()\nSRCS(missing.cpp)\nEND()\n")
        self.assertConfigureLikeYa()

    def test_fatal_message(self):
        self.write("ya.make", "LIBRARY()\nMESSAGE(FATAL_ERROR E2E_FATAL)\nEND()\n")
        self.assertConfigureLikeYa()

    def test_missing_include(self):
        self.write("ya.make", "LIBRARY()\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", '#include "does_not_exist.h"\n')
        self.assertConfigureLikeYa()

    def test_recurse_to_missing_directory(self):
        self.write("ya.make", "RECURSE(missing_dir)\n")
        self.assertConfigureLikeYa()

    def test_missing_module_end(self):
        self.write("ya.make", "LIBRARY()\nSRCS(answer.cpp)\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        self.assertConfigureLikeYa()

    def test_peerdir_loop(self):
        self.write("ya.make", "RECURSE(a b)\n")
        self.write("a/ya.make", "LIBRARY()\nPEERDIR(__ay_e2e__/b)\nEND()\n")
        self.write("b/ya.make", "LIBRARY()\nPEERDIR(__ay_e2e__/a)\nEND()\n")
        self.assertConfigureLikeYa()

    def test_unknown_test_size(self):
        self.write("ya.make", "PY3TEST()\nTEST_SRCS(test_answer.py)\nSIZE(HUGE)\nEND()\n")
        self.write("test_answer.py", "def test_answer():\n    pass\n")
        self.assertConfigureLikeYa()

    def test_clean_library(self):
        self.write("ya.make", "LIBRARY()\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        self.assertConfigureLikeYa()
