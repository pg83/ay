from tst.make.lib import MakeTest


class RecurseBoundariesTest(MakeTest):
    def test_diamond_recurses_keep_one_style_suite(self):
        self.write("ya.make", "RECURSE(first second)\n")
        for name in ("first", "second"):
            self.write(f"{name}/ya.make", "RECURSE(../shared)\n")
        self.write("shared/ya.make", "LIBRARY()\nSTYLE_CPP()\nSRCS(answer.cpp)\nEND()\n")
        self.write("shared/answer.cpp", "int answer() { return 42; }\n")
        self.node(self.graph("-t"), "shared/test-results/clang_format/meta.json")

    def test_recurse_requires_a_build_description(self):
        self.write("ya.make", "LIBRARY()\nSRCS(answer.cpp)\nEND()\nRECURSE(missing)\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        self.assertNotEqual(self.make().returncode, 0)
