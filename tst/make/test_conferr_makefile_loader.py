from tst.make.lib import MakeTest, PROJECT


class MakefileLoaderConfigureErrorsTest(MakeTest):
    """Configure errors of ymake's makefile loader (devtools/ymake/makefile_loader.cpp)."""

    def test_internal_command(self):
        self.write("ya.make", "LIBRARY()\n_E2E_INTERNAL(value)\nEND()\n")
        self.assertConfigureLikeYa()

    def test_command_outside_of_a_module(self):
        self.write("ya.make", "SRCS(answer.cpp)\nLIBRARY()\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        self.assertConfigureLikeYa()

    def test_second_module(self):
        self.write("ya.make", "LIBRARY()\nEND()\nPROGRAM()\nEND()\n")
        self.assertConfigureLikeYa()

    def test_module_inside_a_module(self):
        self.write("ya.make", "LIBRARY()\nPROGRAM()\nEND()\n")
        self.assertConfigureLikeYa()

    def test_include_loop(self):
        self.write("ya.make", "INCLUDE(first.inc)\nLIBRARY()\nEND()\n")
        self.write("first.inc", f"INCLUDE(${{ARCADIA_ROOT}}/{PROJECT}/second.inc)\n")
        self.write("second.inc", f"INCLUDE(${{ARCADIA_ROOT}}/{PROJECT}/first.inc)\n")
        self.assertConfigureLikeYa()

    def test_missing_include(self):
        self.write("ya.make", "INCLUDE(missing.inc)\nLIBRARY()\nEND()\n")
        self.assertConfigureLikeYa()

    def test_recurse_to_a_file(self):
        self.write("ya.make", "RECURSE(value.txt)\n")
        self.write("value.txt", "value\n")
        self.assertConfigureLikeYa()

    def test_unbalanced_parentheses(self):
        self.write("ya.make", "LIBRARY(\nEND()\n")
        self.assertConfigureLikeYa()

    def test_garbage_after_a_statement(self):
        self.write("ya.make", "LIBRARY() garbage\nEND()\n")
        self.assertConfigureLikeYa()
