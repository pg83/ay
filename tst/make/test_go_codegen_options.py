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

    def test_contributed_mockgen_packages_keep_their_public_import_path(self):
        self.write_tree("vendor/ay_e2e/example/ya.make", "GO_LIBRARY()\nVERSION(1.0.0)\nLICENSE(MIT)\nSRCS(api.go)\nEND()\n")
        self.write_tree("vendor/ay_e2e/example/api.go", "package example\ntype Answerer interface { Answer() int }\n")
        self.write("ya.make", "GO_PROGRAM(reflector)\nGO_MOCKGEN_CONTRIB_FROM(ay_e2e/example)\nGO_MOCKGEN_TYPES(Answerer)\nGO_MOCKGEN_REFLECT()\nEND()\n")
        generated = self.node(self.graph(), "main.go")
        args = [arg for command in generated["cmds"] for arg in command["cmd_args"]]
        self.assertIn("ay_e2e/example", args)
        self.assertNotIn("vendor/ay_e2e/example", args)

    def test_go_tests_use_the_declared_package_for_inline_resources(self):
        self.write("ya.make", "GO_TEST(answer)\nSRCS(answer.go)\nGO_TEST_SRCS(answer_test.go)\nRESOURCE(- E2E=42)\nEND()\n")
        self.write("answer.go", "package answer\nfunc Answer() int { return 42 }\n")
        self.write("answer_test.go", 'package answer\nimport "testing"\nfunc TestAnswer(t *testing.T) {}\n')
        graph = self.graph("-t")
        resources = [node for node in graph["graph"] if any(path.startswith(f"$(B)/{PROJECT}/") and path.endswith(".res.go") for path in node["outputs"])]
        self.assertTrue(resources)
        for node in resources:
            args = [arg for command in node["cmds"] for arg in command["cmd_args"]]
            self.assertEqual(args[args.index("-package") + 1], "answer")
