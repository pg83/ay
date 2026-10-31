from tst.make.lib import MakeTest, PROJECT


class MissingGoTestSourcesTest(MakeTest):
    def test_gofmt_omits_missing_go_test_sources_when_keep_going_is_enabled(self):
        self.write("ya.make", "GO_TEST(answer)\nSRCS(answer.go)\nGO_TEST_SRCS(missing_test.go)\nEND()\n")
        self.write("answer.go", "package answer\nfunc Answer() int { return 42 }\n")
        formatted = self.node(self.graph("-t", "-k"), "test-results/gofmt/meta.json")
        args = [arg.replace("$(SOURCE_ROOT)", "$(S)") for command in formatted["cmds"] for arg in command["cmd_args"]]
        self.assertIn(f"$(S)/{PROJECT}/answer.go", args)
        self.assertFalse(any("missing_test.go" in arg for arg in args))
