from tst.make.lib import MakeTest, PROJECT


class GoCodegenOptionsTest(MakeTest):
    def test_openapi_output_suffix_changes_the_generated_filename(self):
        tool = "taxi/infra/go/platform/tools/pkg/deepmap/oapi-codegen/cmd/oapi-codegen"
        self.write_tree(tool + "/ya.make", "PY3_PROGRAM(oapi-codegen)\nPY_SRCS(MAIN main.py)\nEND()\n")
        self.write_tree(tool + "/main.py", 'print("package answer")\n')
        self.write("ya.make", "GO_LIBRARY()\nGO_OAPI_CODEGEN_TAXI(types answer api.yaml OUT_SUFFIX .e2e)\nEND()\n")
        self.write("api.yaml", "openapi: 3.0.0\ninfo:\n  title: Answer\n  version: 1.0.0\npaths: {}\n")
        self.node(self.graph(), "types.e2e.gen.go")

    def test_mockgen_source_keeps_exclusions_and_unparsed_inputs(self):
        self.write("ya.make", f"GO_LIBRARY()\nGO_MOCKGEN_FROM({PROJECT}/source)\nGO_MOCKGEN_EXCLUDE_INTERFACES(Ignored)\nGO_MOCKGEN_SOURCE(api.go IN_NOPARSE config.json)\nEND()\n")
        self.write("source/ya.make", "GO_LIBRARY()\nSRCS(api.go)\nEND()\n")
        self.write("source/api.go", "package source\ntype API interface { Answer() int }\ntype Ignored interface { Ignore() }\n")
        self.write("config.json", "{}\n")
        generated = self.node(self.graph(), "api.source.gen.go")
        args = [arg for command in generated["cmds"] for arg in command["cmd_args"]]
        self.assertIn("-exclude_interfaces=Ignored", args)
        self.assertIn(f"$(S)/{PROJECT}/config.json", generated["inputs"])

    def test_gateway_openapi_can_disable_json_field_names(self):
        self.write("ya.make", "GO_LIBRARY()\nGO_GRPC_GATEWAY_V2_OPENAPI_SRCS(NO_JSON_NAMES_FOR_FIELDS answer.proto)\nEND()\n")
        self.write("answer.proto", f'syntax = "proto3"; package e2e; option go_package = "a.yandex-team.ru/{PROJECT}"; message Answer {{ int32 value = 1; }}\n')
        generated = self.node(self.graph(), "answer.pb.gw.go")
        args = [arg for command in generated["cmds"] for arg in command["cmd_args"]]
        self.assertTrue(any("json_names_for_fields=false" in arg for arg in args), args)
        self.assertTrue(any(path.endswith("answer.swagger.json") for path in generated["outputs"]))
