import json

from tst.make.lib import MakeTest, PROJECT


class GoContextsTest(MakeTest):
    def test_internal_and_external_tests(self):
        self.write("ya.make", f"GO_TEST_FOR({PROJECT}/lib)\nEND()\n")
        self.write("lib/ya.make", "GO_LIBRARY()\nGO_PACKAGE_NAME(answer)\nSRCS(answer.go)\nALL_GO_TEST_SRCS()\nGO_TEST_SRCS(answer_test.go)\nGO_XTEST_SRCS(external_test.go)\nGO_TEST_EMBED_PATTERN(data/*.txt)\nGO_XTEST_EMBED_PATTERN(data/*.txt)\nGO_EMBED_TEST_DIR(ALL data)\nGO_EMBED_XTEST_DIR(ALL data)\nEND()\n")
        self.write("lib/answer.go", "package answer\nfunc Answer() int { return 42 }\n")
        self.write("lib/answer_test.go", 'package answer\nimport "testing"\nfunc TestAnswer(t *testing.T) {}\nfunc TestIgnored(t *testing.T) {}\n')
        self.write("lib/external_test.go", 'package answer_test\nimport "testing"\nfunc TestExternal(t *testing.T) {}\n')
        self.write("lib/data/value.txt", "42\n")
        self.write("lib/data/.hidden", "17\n")
        graph = self.graph("-t")
        self.assertTrue(any("go_test" in json.dumps(node.get("cmds", [])) for node in graph["graph"]))

    def test_generated_test_embeds(self):
        self.write("ya.make", f"GO_TEST_FOR({PROJECT}/lib)\nEND()\n")
        self.write("lib/ya.make", f"GO_LIBRARY()\nSRCS(answer.go)\nGO_TEST_SRCS(answer_test.go)\nGO_XTEST_SRCS(external_test.go)\nCOPY_FILE(value.txt generated/value.txt)\nGO_TEST_EMBED_BINDIR(PREFIX {PROJECT} generated FILES generated/value.txt)\nGO_XTEST_EMBED_BINDIR(PREFIX {PROJECT} generated FILES generated/value.txt)\nEND()\n")
        self.write("lib/answer.go", "package answer\nfunc Answer() int { return 42 }\n")
        self.write("lib/answer_test.go", 'package answer\nimport "testing"\nfunc TestAnswer(t *testing.T) {}\n')
        self.write("lib/external_test.go", 'package answer_test\nimport "testing"\nfunc TestExternal(t *testing.T) {}\n')
        self.write("lib/value.txt", "42\n")
        self.node(self.graph("-t"), "generated/value.txt")

    def test_proto_gateway_outputs(self):
        self.write("answer.proto", f'syntax = "proto3";\npackage e2e;\noption go_package = "a.yandex-team.ru/{PROJECT}";\nmessage Answer {{ int32 value = 1; }}\nservice Answers {{ rpc Get(Answer) returns (Answer); }}\n')
        for macro in ("GO_GRPC_GATEWAY_SWAGGER_SRCS", "GO_GRPC_GATEWAY_V2_OPENAPI_SRCS"):
            with self.subTest(macro=macro):
                self.write("ya.make", f"PROTO_LIBRARY()\nONLY_TAGS(GO_PROTO)\nGRPC()\n{macro}(answer.proto)\nSRCS(answer.proto)\nEND()\n")
                self.node(self.graph(), "answer.swagger.json")

    def test_custom_proto_plugin(self):
        self.write("ya.make", f"PROTO_LIBRARY()\nONLY_TAGS(GO_PROTO)\nGO_PROTO_PLUGIN(mdb-validators .validator.go {PROJECT}/plugin)\nSRCS(answer.proto)\nEND()\n")
        self.write("answer.proto", f'syntax = "proto3";\npackage e2e;\noption go_package = "a.yandex-team.ru/{PROJECT}";\nmessage Answer {{ int32 value = 1; }}\n')
        self.write("plugin/ya.make", "GO_PROGRAM(plugin)\nSRCS(main.go)\nEND()\n")
        self.write("plugin/main.go", "package main\nfunc main() {}\n")
        self.node(self.graph(), "answer.validator.go")

    def test_cgo_options(self):
        self.write("ya.make", "GO_LIBRARY()\nSET(CGO_ENABLED yes)\nCGO_SRCS(answer.go)\nCGO_CFLAGS(-DE2E=42)\nSRCS(answer.c)\nEND()\n")
        self.write("answer.go", 'package answer\n/*\nint answer(void);\n*/\nimport "C"\nfunc Answer() int { return int(C.answer()) }\n')
        self.write("answer.c", "int answer(void) { return E2E; }\n")
        graph = self.graph()
        self.assertTrue(any("-DE2E=42" in json.dumps(node.get("cmds", [])) for node in graph["graph"]))
