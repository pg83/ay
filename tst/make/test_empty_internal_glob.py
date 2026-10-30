from tst.make.lib import MakeTest


class EmptyInternalGlobTest(MakeTest):
    def test_configuration_globs_require_an_output_variable(self):
        core = self.root / "build/ymake.core.conf"
        self.write_tree("build/ymake.core.conf", core.read_text() + "\nmacro E2E_EMPTY_GLOB() {\n    _GLOB()\n}\n")
        self.write("ya.make", "LIBRARY()\nE2E_EMPTY_GLOB()\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        self.assertNotEqual(self.make().returncode, 0)
