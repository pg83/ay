from tst.make.lib import MakeTest, PROJECT


class GeneratorOptionsTest(MakeTest):
    def test_quoted_shell_operators_remain_generator_arguments(self):
        self.write("ya.make", f'LIBRARY()\nRUN_PROGRAM({PROJECT}/tool "&&" ";" STDOUT result.txt)\nRESOURCE(result.txt /result)\nEND()\n')
        self.write("tool/ya.make", "PY3_PROGRAM(tool)\nPY_SRCS(MAIN main.py)\nEND()\n")
        self.write("tool/main.py", "import sys\nprint(sys.argv[1:])\n")
        generated = self.node(self.graph(), "result.txt")
        args = [arg for command in generated["cmds"] for arg in command["cmd_args"]]
        for operator in ("&&", ";"):
            self.assertIn(operator, args)

    def test_empty_expanded_generator_tools_are_configuration_errors(self):
        self.write("ya.make", "LIBRARY()\nSET(E2E_TOOL)\nRUN_PROGRAM(${E2E_TOOL} OUT generated.cpp)\nEND()\n")
        self.assertNotEqual(self.make().returncode, 0)

    def test_python_generator_options(self):
        self.write("ya.make", f"LIBRARY()\nADDINCL({PROJECT})\nRUN_PYTHON3(generate.py ${{BINDIR}}/generated.h IN input.txt IN_NOPARSE data.txt OUT_NOAUTO generated.h OUTPUT_INCLUDES dependency.h TOOL {PROJECT}/tool ENV E2E=42 CWD ${{BINDIR}})\nRESOURCE(generated.h /generated.h)\nEND()\n")
        self.write("generate.py", 'from pathlib import Path\nimport sys\nPath(sys.argv[1]).write_text("#pragma once\\n#include \\\"dependency.h\\\"\\n")\n')
        self.write("input.txt", "42\n")
        self.write("data.txt", "42\n")
        self.write("dependency.h", "#pragma once\n")
        self.write("tool/ya.make", "PY3_PROGRAM(tool)\nPY_SRCS(MAIN main.py)\nEND()\n")
        self.write("tool/main.py", "print(42)\n")
        self.node(self.graph(), "generated.h")

    def test_c_static_analysis(self):
        self.write("ya.make", "LIBRARY()\nSELECT_CLANG_SA_CONFIG(analyzer.json)\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        self.write("analyzer.json", "{}\n")
        self.node(self.graph("-DCLANG_SA_ENABLE=yes"), "answer.cpp.o")

    def test_pire_inline(self):
        self.write("ya.make", "LIBRARY()\nPIRE_INLINE(answer.pire)\nRESOURCE(answer.pire /answer)\nEND()\n")
        self.write("answer.pire", "[0-9]+\n")
        graph = self.graph()
        self.assertTrue(any(f"$(S)/{PROJECT}/answer.pire" in node["inputs"] for node in graph["graph"]))
