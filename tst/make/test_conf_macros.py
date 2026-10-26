from tst.make.lib import MakeTest, PROJECT


BUILDER = "$(B)/sdg/devtools/py_monobinary_builder/py_monobinary_builder"


def args(node):
    return [arg for command in node["cmds"] for arg in command["cmd_args"]]


class ConfMacrosTest(MakeTest):
    def test_body_runs_a_tool_of_the_same_build(self):
        self.write("ya.make", f"PY3_PROGRAM(monobinary)\nPEERDIR({PROJECT}/lib)\nPY_MONOBINARY(answer lib.answer)\nEND()\n")
        self.write("lib/ya.make", "PY3_LIBRARY()\nPY_SRCS(answer.py)\nEND()\n")
        self.write("lib/answer.py", "print(42)\n")
        graph = self.graph()
        generated = self.node(graph, "py_monobinary.gen.py")
        builders = [node for node in graph["graph"] if BUILDER in node["outputs"]]
        self.assertEqual(len(builders), 1)
        self.assertIn(builders[0]["uid"], generated["deps"])
        self.assertEqual(args(generated)[1:], ["--monobinary-name", "monobinary", "--program-mapping", "answer", "lib.answer"])
        self.assertIn(f"$(B)/{PROJECT}/monobinary.sdcmeta", self.node(graph, "monobinary")["outputs"])

    def test_flag_parameter_feeds_a_modifier(self):
        self.write("ya.make", f"PROGRAM(answer)\nSRCS(main.cpp)\nPEERDIR({PROJECT}/lib)\nEND()\n")
        self.write("main.cpp", "int main() { return 0; }\n")
        self.write("lib/ya.make", "LIBRARY()\nALL_SRCS(GLOBAL answer.cpp)\nALL_SRCS(local.cpp)\nEND()\n")
        self.write("lib/answer.cpp", "int answer() { return 42; }\n")
        self.write("lib/local.cpp", "int local() { return 0; }\n")
        graph = self.graph()
        self.assertIn(f"$(B)/{PROJECT}/lib/answer.cpp.o", self.node(graph, f"lib/lib{PROJECT}-lib.global.a")["inputs"])
        self.assertIn(f"$(B)/{PROJECT}/lib/local.cpp.o", self.node(graph, f"lib/lib{PROJECT}-lib.a")["inputs"])

    def test_body_globs_files_for_a_custom_linter(self):
        self.write("ya.make", "LIBRARY()\nSRCS(answer.cpp)\nSTYLE_RUSSIAN_STRINGS(DIRS . EXTRA_PARAMS mode=strict)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        self.write("answer.h", "int answer();\n")
        self.write("canondata/result.cpp", "\n")
        node = self.node(self.graph("-t"), "test-results/russian_strings/meta.json")
        tail = args(node)
        self.assertIn(f"$(S)/{PROJECT}/answer.cpp", node["inputs"])
        self.assertIn(f"$(S)/{PROJECT}/answer.h", node["inputs"])
        self.assertNotIn(f"$(S)/{PROJECT}/canondata/result.cpp", node["inputs"])
        self.assertIn("market/tools/lint/russian_string_linter/wrapper.py", " ".join(tail))
        self.assertIn("mode=strict", " ".join(tail))

    def test_body_nests_glob_variables(self):
        self.write("ya.make", "LIBRARY()\nSRCS(answer.cpp)\nMDS_STYLE_GOTEST_COVERAGE()\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        self.write("answer_test.go", "package answer\n")
        self.write("gotest/ya.make", "GO_TEST()\nEND()\n")
        node = self.node(self.graph("-t"), "test-results/mds_gotest_coverage/meta.json")
        tail = " ".join(args(node))
        self.assertIn(f"$(S)/{PROJECT}/answer_test.go", node["inputs"])
        self.assertIn(f"gotest=${{ARCADIA_ROOT}}/{PROJECT}/gotest/ya.make", tail)
        self.assertIn("gobench=", tail)
