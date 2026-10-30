from tst.make.lib import MakeTest


class GoTestForYsonIncludesTest(MakeTest):
    def test_inherited_yson_generators_search_the_library_schema_directory(self):
        self.write("ya.make", f"GO_TEST_FOR({self.project_name}/lib)\nEND()\n")
        self.write("lib/ya.make", f"GO_LIBRARY(answer)\nPROTO_NAMESPACE({self.project_name}/lib)\nYT_ORM_PROTO_YSON({self.project_name}/lib/answer.proto)\nSRCS(answer.go)\nGO_TEST_SRCS(answer_test.go)\nEND()\n")
        self.write("lib/answer.proto", 'syntax = "proto2"; package e2e; message Answer {}\n')
        self.write("lib/answer.go", "package answer\nfunc Answer() int { return 42 }\n")
        self.write("lib/answer_test.go", 'package answer\nimport "testing"\nfunc TestAnswer(t *testing.T) {}\n')
        generated = self.node(self.graph("-t"), "answer.yson.go")
        args = [arg.replace("$(SOURCE_ROOT)", "$(S)") for command in generated["cmds"] for arg in command["cmd_args"]]
        self.assertIn(f"-I=$(S)/{self.project_name}/lib", args)
        self.assertIn(f"$(S)/{self.project_name}/lib/answer.proto", generated["inputs"])
