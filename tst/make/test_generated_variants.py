from tst.make.lib import MakeTest, PROJECT


class GeneratedVariantsTest(MakeTest):
    def test_declare_recursive_inputs(self):
        self.write("ya.make", "LIBRARY()\nDECLARE_IN_DIRS(DATA *.txt RECURSIVE DIRS data)\nRUN_PYTHON3(generate.py IN ${DATA_FILES} STDOUT generated.cpp)\nEND()\n")
        self.write("generate.py", 'print("int answer() { return 42; }")\n')
        self.write("data/one.txt", "1\n")
        self.write("data/nested/two.txt", "2\n")
        graph = self.graph()
        inputs = self.node(graph, "generated.cpp")["inputs"]
        self.assertIn(f"$(S)/{PROJECT}/data/one.txt", inputs)
        self.assertIn(f"$(S)/{PROJECT}/data/nested/two.txt", inputs)

    def test_run_program_output(self):
        self.write("ya.make", f"LIBRARY()\nRUN_PROGRAM({PROJECT}/generator IN value.txt STDOUT generated.cpp ENV E2E=42)\nEND()\n")
        self.write("value.txt", "42\n")
        self.write("generator/ya.make", "PY3_PROGRAM(generator)\nPY_SRCS(MAIN main.py)\nEND()\n")
        self.write("generator/main.py", 'print("int answer() { return 42; }")\n')
        graph = self.graph()
        self.node(graph, "generated.cpp")
        self.node(graph, "generated.cpp.o")

    def test_run_program_input_dot_segment(self):
        self.write("ya.make", f"LIBRARY()\nSET(DATA ${{ARCADIA_ROOT}}/{PROJECT}/data)\nSET_APPEND(FILES ${{DATA}}/./nested/value.txt)\n"
                   f"RUN_PROGRAM({PROJECT}/generator \"${{FILES}}\" IN ${{FILES}} STDOUT generated.cpp)\nEND()\n")
        self.write("data/nested/value.txt", "42\n")
        self.write("generator/ya.make", "PY3_PROGRAM(generator)\nPY_SRCS(MAIN main.py)\nEND()\n")
        self.write("generator/main.py", 'print("int answer() { return 42; }")\n')
        node = self.node(self.graph(), "generated.cpp")
        self.assertEqual([path for path in node["inputs"] if f"/{PROJECT}/data/" in path], [f"$(S)/{PROJECT}/data/nested/value.txt"])
        args = [arg.replace("$(SOURCE_ROOT)", "$(S)") for command in node["cmds"] for arg in command["cmd_args"]]
        self.assertIn(f"$(S)/{PROJECT}/data/./nested/value.txt", args)

    def test_enum_serialization(self):
        self.write("ya.make", "LIBRARY()\nGENERATE_ENUM_SERIALIZATION_WITH_HEADER(value.h)\nEND()\n")
        self.write("value.h", "#pragma once\nenum class Value { Answer = 42, Other = 17 };\n")
        graph = self.graph()
        self.assertTrue(any(f"$(S)/{PROJECT}/value.h" in node["inputs"] and any(path.endswith(".cpp") for path in node["outputs"]) for node in graph["graph"]))

    def test_ragel5(self):
        self.write("ya.make", "LIBRARY()\nSRCS(machine.rl5)\nEND()\n")
        self.write("machine.rl5", '%%{ machine scanner; main := any*; }%%\n%% write data;\n')
        self.node(self.graph(), "machine.rl5.cpp")

    def test_yql_library(self):
        self.write("ya.make", "YQL_LIBRARY(answer)\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        graph = self.graph()
        self.assertTrue(any("abi" in path and path.endswith(".a") for node in graph["graph"] if node["uid"] in graph["result"] for path in node["outputs"]))
