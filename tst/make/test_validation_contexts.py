from tst.make.lib import MakeTest, PROJECT


class ValidationContextsTest(MakeTest):
    def test_custom_cpp_style_restrictions(self):
        self.write("answer.cpp", "int answer() { return 42; }\n")
        for macro in ("STYLE_CPP_USER_SESSIONS()", "STYLE_CPP_YT()", "STYLE_CPP(CONFIG_TYPE unknown)", "STYLE_CPP(CONFIG_TYPE .clang-format)"):
            with self.subTest(macro=macro):
                self.write("ya.make", f"LIBRARY()\nSRCS(answer.cpp)\n{macro}\nEND()\n")
                self.assertNotEqual(self.make("-t").returncode, 0)

    def test_configured_cpp_style(self):
        self.write("ya.make", f"LIBRARY()\nSET(MODULE_COMMON_CONFIGS_DIR {PROJECT}/config)\nSTYLE_CPP(CONFIG_TYPE .clang-format)\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        self.write("config/.clang-format", "BasedOnStyle: LLVM\n")
        self.graph("-t")

    def test_python_style_restrictions(self):
        self.write("answer.py", "answer = 42\n")
        for macro in ("STYLE_RUFF(RUN_IN_SOURCE_ROOT)", "STYLE_RUFF(CONFIG_TYPE unknown)", "STYLE_RUFF(CONFIG_TYPE ruff.toml)", "STYLE_PYTHON(CONFIG_TYPE unknown)"):
            with self.subTest(macro=macro):
                self.write("ya.make", f"PY3_LIBRARY()\nPY_SRCS(answer.py)\n{macro}\nEND()\n")
                self.assertNotEqual(self.make("-t").returncode, 0)

    def test_copy_argument_validation(self):
        self.write("answer.cpp", "int answer() { return 42; }\n")
        for args in ("answer.cpp FROM", "answer.cpp FROM DESTINATION generated", "answer.cpp AUTO AUTO", "answer.cpp OUTPUT_INCLUDES value.h AUTO unexpected.cpp"):
            with self.subTest(args=args):
                self.write("ya.make", f"LIBRARY()\nCOPY({args})\nSRCS(answer.cpp)\nEND()\n")
                self.assertNotEqual(self.make().returncode, 0)

    def test_declare_directory_roots_and_exclusions(self):
        self.write("generate.py", "print(42)\n")
        self.write("data/nested/value.txt", "42\n")
        self.write("data/nested/ignored.txt", "17\n")
        for root in ("data", f"${{ARCADIA_ROOT}}/{PROJECT}/data"):
            with self.subTest(root=root):
                self.write("ya.make", f"LIBRARY()\nDECLARE_IN_DIRS(DATA *.txt SRCDIR {root} DIRS nested RECURSIVE EXCLUDES ignored.txt)\nRUN_PYTHON3(generate.py IN ${{DATA_FILES}} STDOUT value.txt)\nRESOURCE(value.txt /value)\nEND()\n")
                self.node(self.graph(), "value.txt")

    def test_antlr_output_includes_are_not_command_options(self):
        self.write("ya.make", f"LIBRARY()\nADDINCL({PROJECT} contrib/libs/antlr4_cpp_runtime/src)\nRUN_ANTLR4_CPP(Answer.g4 -Werror NO_LISTENER OUTPUT_INCLUDES extra.h IN input.txt OUT_NOAUTO extra.txt)\nEND()\n")
        self.write("Answer.g4", "grammar Answer; answer: NUMBER EOF; NUMBER: [0-9]+;\n")
        self.write("extra.h", "#pragma once\n")
        self.write("input.txt", "42\n")
        result = self.make()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("include", result.stderr)

        for file in ("IN", "OUT_NOAUTO", "extra.txt"):
            self.write(file, "#pragma once\n")
        graph = self.graph()
        inputs = self.node(graph, "AnswerLexer.g4.cpp.o")["inputs"]
        for file in ("IN", "OUT_NOAUTO", "input.txt"):
            self.assertIn(f"$(S)/{PROJECT}/{file}", inputs)

    def test_python_main_without_function(self):
        self.write("ya.make", "PY3_PROGRAM(answer)\nPY_SRCS(main.py)\nPY_MAIN(main)\nNO_IMPORT_TRACING()\nEND()\n")
        self.write("main.py", "def main():\n    print(42)\n")
        self.node(self.graph(), "answer")

    def test_empty_generator_dependency_cycle(self):
        self.write("ya.make", "LIBRARY()\nCOPY_FILE(second.cpp first.cpp)\nCOPY_FILE(first.cpp second.cpp)\nSRCS(first.cpp)\nEND()\n")
        self.assertNotEqual(self.make().returncode, 0)
