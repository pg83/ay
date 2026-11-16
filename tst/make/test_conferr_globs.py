from tst.make.lib import MakeTest


class GlobConfigureErrorsTest(MakeTest):
    """_GLOB and _LATE_GLOB are internal commands (devtools/ymake/makefile_loader.cpp). Their RESTRICTIONS
    parse errors (glob_helper.cpp) are unreachable from ya.make: only the conf passes them, with fixed values."""

    def library(self, *statements):
        self.write("ya.make", "LIBRARY()\n" + "".join(statement + "\n" for statement in statements) + "SRCS(value.cpp)\nEND()\n")
        self.write("value.cpp", "int value() { return 42; }\n")

    def test_glob_in_ya_make(self):
        self.library("_GLOB(E2E_FILES *.cpp RESTRICTIONS MAX_MATCHES e2e)")
        self.assertConfigureLikeYa()

    def test_late_glob_in_ya_make(self):
        self.library("_LATE_GLOB(E2E_FILES *.cpp RESTRICTIONS MAX_WATCH_DIRS 0)")
        self.assertConfigureLikeYa()
