from tst.make.lib import MakeTest, PROJECT


class CommandDetailsTest(MakeTest):
    def test_go_proto_plugin_environment(self):
        self.write("answer.proto", f'syntax = "proto3"; package e2e; option go_package = "a.yandex-team.ru/{PROJECT}"; message Answer {{}}\n')
        self.write("plugin/ya.make", "GO_PROGRAM(plugin)\nSRCS(main.go)\nEND()\n")
        self.write("plugin/main.go", "package main\nfunc main() {}\n")
        for plugin in ("custom", "mdb-validators", "proto_validation", "mdb-dynforms", "custom-mdb-validators"):
            with self.subTest(plugin=plugin):
                self.write("ya.make", f"PROTO_LIBRARY()\nONLY_TAGS(GO_PROTO)\nGO_PROTO_PLUGIN({plugin} .extra.go {PROJECT}/plugin)\nSRCS(answer.proto)\nEND()\n")
                node = self.node(self.graph(), "answer.pb.go")
                env = {key: value.replace("$(BUILD_ROOT)", "$(B)") for key, value in node["env"].items()}
                if plugin == "custom":
                    self.assertNotIn("GOCACHE", env)
                else:
                    self.assertEqual(env["GOCACHE"], f"$(B)/{PROJECT}/.gocache")
                    self.assertRegex(env["GOROOT"].replace("$(B)/resources/GO_TOOLS", "$(GO_TOOLS)"), r"^\$\(GO_TOOLS[-)]")
                    self.assertEqual(env["PATH"], env["GOROOT"] + "/bin")
                self.assertEqual(node["cmds"][0]["env"], node["env"])

    def test_go_test_relative_debug_paths(self):
        self.write("answer.go", "package answer\nfunc Answer() int { return 42 }\n")
        self.write("answer_test.go", 'package answer\nimport "testing"\nfunc TestAnswer(t *testing.T) {}\n')
        self.write("answer.s", "TEXT ·answerAsm(SB),$0-0\n    RET\n")
        for module in ("GO_TEST(gotest)", "GO_LIBRARY()"):
            for relative in ("no", "yes"):
                with self.subTest(module=module, relative=relative):
                    option = "ENABLE(GO_DEBUG_PATH_RELATIVE)\n" if relative == "yes" else ""
                    tests = "GO_TEST_SRCS(answer_test.go)\n" if module.startswith("GO_TEST") else ""
                    self.write("ya.make", f"{module}\n{option}SRCS(answer.go answer.s)\n{tests}END()\n")
                    graph = self.graph("-t")
                    output = "gotest" if tests else f"{PROJECT}.a"
                    args = [arg for command in self.node(graph, output)["cmds"] for arg in command["cmd_args"]]
                    self.assertEqual(args[args.index("++debug-root-map") + 1], "source=;build=;tools=" if relative == "yes" else "source=/-S;build=/-B;tools=/-T")
                    args = [arg.replace("$(SOURCE_ROOT)", "$(S)").replace("$(BUILD_ROOT)", "$(B)") for command in self.node(graph, "gen.symabis")["cmds"] for arg in command["cmd_args"]]
                    self.assertEqual(args[args.index("-trimpath") + 1], "$(S)=>;$(B)=>;$(TOOL_ROOT)=>" if relative == "yes" else "$(S)=>/-S;$(B)=>/-B;$(TOOL_ROOT)=>/-T")

    def test_quoted_inline_resource_command(self):
        self.write("ya.make", 'PY3_LIBRARY()\nSET(ANSWER 42)\nSET(EMPTY)\nRESOURCE(- ANSWER=\\"${ANSWER}\\")\nRESOURCE(- EMPTY=\\"${EMPTY}\\")\nEND()\n')
        graph = self.graph()
        values = []
        for node in graph["graph"]:
            if any(output.startswith(f"$(B)/{PROJECT}/objcopy_") for output in node["outputs"]):
                for command in node["cmds"]:
                    args = command["cmd_args"]
                    if "--kvs" in args:
                        values.extend(args[args.index("--kvs") + 1:])
        for value in ("ANSWER=42", "EMPTY="):
            self.assertIn(value, values)

    def test_exectest_dns_requirement(self):
        self.write("ya.make", "EXECTEST()\nRUN(echo answer)\nREQUIREMENTS(network:full dns:dns64)\nEND()\n")
        node = self.node(self.graph("-t"), "test-results/exectest/meta.json")
        self.assertEqual(node["requirements"]["dns"], "dns64")
        self.assertEqual(node["requirements"]["network"], "full")
