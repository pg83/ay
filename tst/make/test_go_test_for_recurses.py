from tst.make.lib import MakeTest, PROJECT


class GoTestForRecursesTest(MakeTest):
    def test_inherited_library_recurses_do_not_select_extra_test_modules(self):
        self.write("ya.make", f"GO_TEST_FOR({PROJECT}/lib)\nEND()\n")
        self.write("lib/ya.make", "GO_LIBRARY(answer)\nSRCS(answer.go)\nGO_TEST_SRCS(answer_test.go)\nEND()\nRECURSE_FOR_TESTS(child)\n")
        self.write("lib/answer.go", "package answer\nfunc Answer() int { return 42 }\n")
        self.write("lib/answer_test.go", 'package answer\nimport "testing"\nfunc TestAnswer(t *testing.T) {}\n')
        self.write("lib/child/ya.make", "MESSAGE(FATAL_ERROR inherited_recurse_was_visited)\n")
        inputs = {path for node in self.graph("-t")["graph"] for path in node["inputs"]}
        self.assertIn(f"$(S)/{PROJECT}/lib/answer_test.go", inputs)
