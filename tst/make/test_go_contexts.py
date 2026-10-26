import json

from tst.make.lib import MakeTest, PROJECT


class GoContextsTest(MakeTest):
    def test_resource_imports_precede_source_imports(self):
        self.write("ya.make", "RECURSE(app/gotest)\n")
        self.write("app/ya.make", "GO_PROGRAM(tool)\nALL_GO_SRCS()\nALL_GO_TEST_SRCS()\nEND()\n")
        self.write("app/main.go", f'package main\nimport _ "a.yandex-team.ru/{PROJECT}/source_dep"\nfunc main() {{}}\n')
        self.write("app/main_test.go", 'package main\nimport "testing"\nfunc TestAnswer(t *testing.T) {}\n')
        self.write("app/gotest/ya.make", f"GO_TEST_FOR({PROJECT}/app)\nGO_PACKAGE_NAME(main)\nRESOURCE(${{ARCADIA_ROOT}}/{PROJECT}/data/embedded.go /data)\nEND()\n")
        self.write("data/embedded.go", f'package embedded\nimport _ "a.yandex-team.ru/{PROJECT}/resource_dep"\n')
        for name in ("source_dep", "resource_dep"):
            self.write(name + "/ya.make", "GO_LIBRARY()\nSRCS(answer.go)\nEND()\n")
            self.write(name + "/answer.go", f"package {name}\n")
        node = self.node(self.graph("-t"), "app/gotest/tool")
        args = next(command["cmd_args"] for command in node["cmds"] if "++peers" in command["cmd_args"])
        peers = args[args.index("++peers") + 1:args.index("++non-local-peers")]
        self.assertLess(peers.index(f"{PROJECT}/resource_dep/resource_dep.a"), peers.index(f"{PROJECT}/source_dep/source_dep.a"))

    def test_program_test_resource_uses_explicit_package(self):
        self.write("ya.make", "RECURSE(app/gotest)\n")
        self.write("app/ya.make", f"GO_PROGRAM(tool)\nRESOURCE(${{ARCADIA_ROOT}}/{PROJECT}/app/value.txt /parent)\nALL_GO_SRCS()\nALL_GO_TEST_SRCS()\nEND()\n")
        self.write("app/main.go", "package main\nfunc main() {}\n")
        self.write("app/value.txt", "17\n")
        self.write("app/main_test.go", 'package main\nimport "testing"\nfunc TestAnswer(t *testing.T) {}\n')
        self.write("app/gotest/ya.make", f"GO_TEST_FOR({PROJECT}/app)\nGO_PACKAGE_NAME(main)\nRESOURCE(value.txt /value)\nEND()\n")
        self.write("app/gotest/value.txt", "42\n")
        graph = self.graph("-t")
        node = next(node for node in graph["graph"] if f"$(S)/{PROJECT}/app/value.txt" in node["inputs"] and any(path.startswith(f"$(B)/{PROJECT}/app/gotest/") and path.endswith(".res.go") for path in node["outputs"]))
        args = [arg for command in node["cmds"] for arg in command["cmd_args"]]
        self.assertEqual(args[args.index("-package") + 1], "main")

    def test_program_test_keeps_resource_imports(self):
        self.write("ya.make", "RECURSE(app/gotest)\n")
        self.write("app/ya.make", "GO_PROGRAM(tool)\nALL_GO_SRCS()\nALL_GO_TEST_SRCS()\nEND()\n")
        self.write("app/main.go", "package main\nfunc main() {}\n")
        self.write("app/main_test.go", 'package main\nimport "testing"\nfunc TestAnswer(t *testing.T) {}\n')
        self.write("app/gotest/ya.make", f"GO_TEST_FOR({PROJECT}/app)\nGO_PACKAGE_NAME(main)\nRESOURCE(${{ARCADIA_ROOT}}/{PROJECT}/data/embedded.go /data)\nEND()\n")
        self.write("data/embedded.go", f'package embedded\nimport _ "a.yandex-team.ru/{PROJECT}/dependency"\n')
        self.write("dependency/ya.make", "GO_LIBRARY()\nSRCS(answer.go)\nEND()\n")
        self.write("dependency/answer.go", "package dependency\nfunc Answer() int { return 42 }\n")
        graph = self.graph("-t")
        node = self.node(graph, "app/gotest/tool")
        self.assertIn(f"$(B)/{PROJECT}/dependency/dependency.a", node["inputs"])

    def test_java_recurse_collects_proto_go_checks(self):
        self.write("ya.make", "JAVA_PROGRAM(answer)\nJDK_VERSION(17)\nJAVA_SRCS(Main.java)\nEND()\nRECURSE(schema)\n")
        self.write("Main.java", "public class Main {}\n")
        self.write("schema/ya.make", "PROTO_LIBRARY()\nEXCLUDE_TAGS(PY_PROTO PY3_PROTO)\nSRCS(answer.proto)\nEND()\n")
        self.write("schema/answer.proto", f'syntax = "proto3";\npackage e2e;\noption go_package = "a.yandex-team.ru/{PROJECT}/schema";\nmessage Answer {{}}\n')
        graph = self.graph("-t", "--add-modules-to-results")
        self.node(graph, "schema/test-results/govet/meta.json")

    def test_cgo_test_keeps_source_module_context(self):
        self.write("ya.make", "RECURSE(lib/gotest)\n")
        self.write("lib/gotest/ya.make", f"GO_TEST_FOR({PROJECT}/lib)\nEND()\n")
        self.write("lib/ya.make", "GO_LIBRARY()\nCGO_SRCS(answer.go)\nGO_TEST_SRCS(answer_test.go)\nEND()\n")
        self.write("lib/answer.go", 'package answer\n/*\n#include "answer.h"\n*/\nimport "C"\nfunc Answer() int { return int(C.answer()) }\n')
        self.write("lib/answer.h", "int answer(void);\n")
        self.write("lib/answer_test.go", 'package answer\nimport "testing"\nfunc TestAnswer(t *testing.T) {}\n')
        graph = self.graph("-t")
        node = self.node(graph, "lib/gotest/answer.cgo1.go")
        args = [arg.replace("$(SOURCE_ROOT)", "$(S)") for command in node["cmds"] for arg in command["cmd_args"]]
        self.assertIn(f"a.yandex-team.ru/{PROJECT}/lib", args)
        self.assertIn(f"-I$(S)/{PROJECT}/lib", args)

    def test_library_link_flags(self):
        self.write("ya.make", "GO_LIBRARY()\nGO_LINK_FLAGS(-X e2e.version=42)\nSRCS(answer.go)\nEND()\n")
        self.write("answer.go", 'package e2e\nvar version string\n')
        graph = self.graph()
        node = next(node for node in graph["graph"] if f"$(S)/{PROJECT}/answer.go" in node["inputs"] and any(path.endswith(".a") for path in node["outputs"]))
        args = [arg for command in node["cmds"] for arg in command["cmd_args"]]
        self.assertIn("e2e.version=42", args)

    def test_cgo_link_flags_on_go_program(self):
        self.write("ya.make", "GO_PROGRAM(answer)\nCGO_LDFLAGS(-Wl,-z,copyreloc)\nSRCS(main.go)\nEND()\n")
        self.write("main.go", "package main\nfunc main() {}\n")
        node = self.node(self.graph(), "answer")
        args = [arg for command in node["cmds"] for arg in command["cmd_args"]]
        self.assertIn("-Wl,-z,copyreloc", args)
        extldflags = args[args.index("++extldflags") + 1:args.index("++peers")]
        for flag in ("-lpthread", "-lresolv"):
            self.assertEqual(extldflags.count(flag), 1, flag)

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
