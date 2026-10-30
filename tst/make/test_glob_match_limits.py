from tst.make.lib import MakeTest


class GlobMatchLimitsTest(MakeTest):
    def test_configuration_globs_enforce_their_explicit_match_limit(self):
        core = self.root / "build/ymake.core.conf"
        self.write_tree("build/ymake.core.conf", core.read_text() + "\nmacro E2E_LIMITED_GLOB() {\n    _GLOB(E2E_FILES *.txt RESTRICTIONS MAX_MATCHES 1)\n}\n")
        self.write("ya.make", "LIBRARY()\nE2E_LIMITED_GLOB()\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        self.write("first.txt", "first\n")
        self.write("second.txt", "second\n")
        self.assertNotEqual(self.make().returncode, 0)
