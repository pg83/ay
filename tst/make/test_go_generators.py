from tst.make.lib import MakeTest, PROJECT


class GoGeneratorsTest(MakeTest):
    def test_mock_reflector(self):
        self.write("ya.make", f"GO_PROGRAM(reflector)\nGO_MOCKGEN_FROM({PROJECT}/source)\nGO_MOCKGEN_TYPES(Answerer)\nGO_MOCKGEN_REFLECT()\nEND()\n")
        self.write("source/ya.make", "GO_LIBRARY()\nSRCS(api.go)\nEND()\n")
        self.write("source/api.go", "package source\ntype Answerer interface { Answer() int }\n")
        self.node(self.graph(), "main.go")

    def test_mock_reflected_interfaces(self):
        self.write("ya.make", f"GO_LIBRARY()\nGO_MOCKGEN_FROM({PROJECT}/source)\nGO_MOCKGEN_PACKAGE(mocks)\nGO_MOCKGEN_TYPES(Answerer)\nGO_MOCKGEN_EXCLUDE_INTERFACES(Ignored)\nGO_MOCKGEN_MOCKS()\nEND()\n")
        self.write("gen/ya.make", f"GO_PROGRAM(reflector)\nGO_MOCKGEN_FROM({PROJECT}/source)\nGO_MOCKGEN_TYPES(Answerer)\nGO_MOCKGEN_REFLECT()\nEND()\n")
        self.write("source/ya.make", "GO_LIBRARY()\nSRCS(api.go)\nEND()\n")
        self.write("source/api.go", "package source\ntype Answerer interface { Answer() int }\ntype Ignored interface { Ignore() }\n")
        self.node(self.graph(), "main.go")

    def test_openapi_v2(self):
        self.write("ya.make", "GO_LIBRARY()\nGO_OAPI_CODEGEN_V2(types answer api.yaml IN_NOPARSE data.json)\nEND()\n")
        self.write("api.yaml", "openapi: 3.0.0\ninfo:\n  title: Answer\n  version: 1.0.0\npaths: {}\ncomponents:\n  schemas:\n    Answer:\n      type: integer\n")
        self.write("data.json", "{}\n")
        self.node(self.graph(), "types.gen.go")

    def test_openapi_taxi_1134_generated_spec(self):
        self.write("ya.make", f"GO_LIBRARY()\nRUN_PROGRAM({PROJECT}/generator IN api.yaml OUT_NOAUTO ${{BINDIR}}/resolved.yaml)\n"
                   f"GO_OAPI_CODEGEN_TAXI_1134(${{BINDIR}}/resolved.yaml answer --patch-version v1 GENERATE types PACKAGE answer TEMPLATES ${{ARCADIA_ROOT}}/{PROJECT}/templates IN_NOPARSE ${{ARCADIA_ROOT}}/{PROJECT}/templates/imports.tmpl)\n"
                   f"GO_OAPI_CODEGEN_TAXI_1134(${{BINDIR}}/resolved.yaml models --patch-version v1 PACKAGE answer CONFIG ${{ARCADIA_ROOT}}/{PROJECT}/config.yaml TEMPLATES ${{ARCADIA_ROOT}}/{PROJECT}/templates --strict\n"
                   f"    IN_NOPARSE {PROJECT}/config.yaml ${{ARCADIA_ROOT}}/{PROJECT}/templates/imports.tmpl ${{BINDIR}}/resolved.yaml)\nEND()\n")
        self.write("api.yaml", "openapi: 3.0.0\ninfo:\n  title: Answer\n  version: 1.0.0\npaths: {}\n")
        self.write("config.yaml", "output-options: {}\n")
        self.write("templates/imports.tmpl", "package {{ .PackageName }}\n")
        self.write("generator/ya.make", "PY3_PROGRAM(generator)\nPY_SRCS(MAIN main.py)\nEND()\n")
        self.write("generator/main.py", "print('generator')\n")
        graph = self.graph()
        spec = self.node(graph, "resolved.yaml")
        for output, expected, inputs in (
            ("answer.gen.go", ["--generate", "types", "--package", "answer", "--templates", f"$(S)/{PROJECT}/templates", "--patch-version", "v1", f"$(B)/{PROJECT}/resolved.yaml"],
             [f"$(B)/{PROJECT}/resolved.yaml", f"$(S)/{PROJECT}/templates/imports.tmpl"]),
            ("models.gen.go", ["--package", "answer", "--templates", f"$(S)/{PROJECT}/templates", "--config", f"$(S)/{PROJECT}/config.yaml", "--patch-version", "v1", "--strict", f"$(B)/{PROJECT}/resolved.yaml"],
             [f"$(B)/{PROJECT}/resolved.yaml", f"$(S)/{PROJECT}/config.yaml", f"$(S)/{PROJECT}/templates/imports.tmpl"]),
        ):
            node = self.node(graph, output)
            args = [arg.replace("$(SOURCE_ROOT)", "$(S)").replace("$(BUILD_ROOT)", "$(B)") for command in node["cmds"] for arg in command["cmd_args"]]
            self.assertEqual(args[1:], expected)
            self.assertEqual(sorted(path for path in node["inputs"] if f"/{PROJECT}/" in path), inputs)
            self.assertIn(spec["uid"], node["deps"])
        library = self.node(graph, f"{PROJECT}.a")
        args = [arg.replace("$(BUILD_ROOT)", "$(B)") for command in library["cmds"] for arg in command["cmd_args"]]
        self.assertEqual([arg for arg in args if arg.endswith(".gen.go")], [f"$(B)/{PROJECT}/answer.gen.go", f"$(B)/{PROJECT}/models.gen.go"])

    def test_gateway(self):
        self.write("ya.make", "PROTO_LIBRARY()\nONLY_TAGS(GO_PROTO)\nGRPC()\nGO_GRPC_GATEWAY_V2_SRCS(answer.proto)\nSRCS(answer.proto)\nEND()\n")
        self.write("answer.proto", f'syntax = "proto3"; package e2e; option go_package = "a.yandex-team.ru/{PROJECT}"; message Answer {{ int32 value = 1; }} service Answers {{ rpc Get(Answer) returns (Answer); }}\n')
        self.node(self.graph(), "answer.pb.gw.go")

    def test_peerdir_statement_precedes_macro_peers(self):
        self.write("ya.make", f"GO_LIBRARY()\nPROTO_NAMESPACE({PROJECT})\nYT_ORM_PROTO_YSON({PROJECT}/answer.proto OUT_OPTS x=y)\nPEERDIR({PROJECT}/peer)\nEND()\n")
        self.write("answer.proto", 'syntax = "proto3"; package e2e; message Answer { int32 value = 1; }\n')
        self.write("peer/ya.make", f"GO_LIBRARY()\nPROTO_NAMESPACE({PROJECT}/peer)\nSRCS(peer.go)\nEND()\n")
        self.write("peer/peer.go", "package peer\n")
        node = self.node(self.graph(), "answer.yson.go")
        includes = [arg.replace("$(SOURCE_ROOT)", "$(S)") for command in node["cmds"] for arg in command["cmd_args"] if arg.startswith("-I=")]
        self.assertEqual(includes[-3:], [f"-I=$(S)/{PROJECT}/peer", "-I=$(S)/contrib/libs/protobuf/src", "-I=$(S)"])
