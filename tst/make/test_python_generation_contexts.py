from tst.make.lib import MakeTest, PROJECT


def args(node):
    return [arg.replace("$(SOURCE_ROOT)", "$(S)").replace("$(BUILD_ROOT)", "$(B)") for command in node["cmds"] for arg in command["cmd_args"]]


class PythonGenerationContextsTest(MakeTest):
    def test_generated_python_scripts_keep_their_producer_dependency(self):
        self.write("ya.make", "LIBRARY()\nCOPY_FILE(template.py generate.py)\nRUN_PYTHON3(${BINDIR}/generate.py STDOUT generated.cpp)\nEND()\n")
        self.write("template.py", 'print("int answer() { return 42; }")\n')
        graph = self.graph()
        script = self.node(graph, "generate.py")
        generated = self.node(graph, "generated.cpp")
        self.assertIn(script["uid"], generated["deps"])
        self.assertIn(f"$(B)/{PROJECT}/generate.py", generated["inputs"])

    def test_generated_go_outputs_are_collected_without_explicit_sources(self):
        self.write("ya.make", "GO_LIBRARY()\nRUN_PYTHON3(generate.py OUT_NOAUTO generated.go)\nEND()\n")
        self.write("generate.py", 'from pathlib import Path\nPath("generated.go").write_text("package answer\\n")\n')
        graph = self.graph()
        generated = self.node(graph, "generated.go")
        library = self.node(graph, f"{PROJECT}.a")
        self.assertIn(generated["uid"], library["deps"])
        self.assertIn(f"$(B)/{PROJECT}/generated.go", library["inputs"])

    def test_environment_names_without_values_are_empty(self):
        self.write("ya.make", "LIBRARY()\nRUN_PYTHON3(generate.py ENV E2E_EMPTY STDOUT generated.cpp)\nEND()\n")
        self.write("generate.py", 'print("int answer() { return 42; }")\n')
        generated = self.node(self.graph(), "generated.cpp")
        self.assertEqual(generated["cmds"][0]["env"]["E2E_EMPTY"], "")

    def test_named_input_arguments_use_the_resolved_input_path(self):
        self.write("ya.make", "LIBRARY()\nRUN_PYTHON3(generate.py DATA=value.txt IN value.txt STDOUT generated.cpp)\nEND()\n")
        self.write("generate.py", 'print("int answer() { return 42; }")\n')
        self.write("value.txt", "42\n")
        self.assertIn(f"DATA=$(S)/{PROJECT}/value.txt", args(self.node(self.graph(), "generated.cpp")))

    def test_additional_tools_are_resolved_in_arguments_and_deduplicated(self):
        self.write("ya.make", f"LIBRARY()\nRUN_PYTHON3(generate.py {PROJECT}/tool {PROJECT}/tool-long TOOL {PROJECT}/tool {PROJECT}/tool-long {PROJECT}/tool STDOUT generated.cpp)\nEND()\n")
        self.write("generate.py", 'print("int answer() { return 42; }")\n')
        for name in ("tool", "tool-long"):
            self.write(name + "/ya.make", "PY3_PROGRAM(generator)\nPY_SRCS(MAIN main.py)\nEND()\n")
            self.write(name + "/main.py", "print(42)\n")
        graph = self.graph()
        generated = self.node(graph, "generated.cpp")
        for name in ("tool", "tool-long"):
            tool = self.node(graph, name + "/generator")
            self.assertIn(f"$(B)/{PROJECT}/{name}/generator", args(generated))
            self.assertEqual(generated["deps"].count(tool["uid"]), 1)

    def test_generated_proto_outputs_are_compiled_automatically(self):
        self.write("ya.make", "LIBRARY()\nRUN_PYTHON3(generate.py OUT answer.proto)\nEND()\n")
        self.write("generate.py", 'from pathlib import Path\nPath("answer.proto").write_text(\'syntax = "proto3"; message Answer {}\\n\')\n')
        graph = self.graph()
        generated = self.node(graph, "answer.proto")
        compiled = self.node(graph, "answer.pb.cc")
        self.assertIn(generated["uid"], compiled["deps"])
        self.assertIn(f"$(B)/{PROJECT}/answer.proto", compiled["inputs"])
