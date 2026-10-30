from tst.make.lib import MakeTest, PROJECT


class AntlrOutputContextsTest(MakeTest):
    def test_declared_output_arguments_are_rooted_in_the_build_directory(self):
        self.grammar()
        self.write("ya.make", "LIBRARY()\nRUN_ANTLR4(Answer.g4 -Dlanguage=Cpp AnswerParser.cpp IN Answer.g4 OUT AnswerParser.cpp)\nEND()\n")
        generated = self.node(self.graph(), "AnswerParser.cpp")
        args = [arg.replace("$(BUILD_ROOT)", "$(B)") for command in generated["cmds"] for arg in command["cmd_args"]]
        self.assertIn(f"$(B)/{PROJECT}/AnswerParser.cpp", args)
        self.assertNotIn("AnswerParser.cpp", args)

    def grammar(self):
        self.write("Answer.g4", "grammar Answer; answer: NUMBER EOF; NUMBER: [0-9]+;\n")

    def test_python_antlr_additional_inputs_are_generator_dependencies(self):
        self.grammar()
        self.write("ya.make", "PY3_LIBRARY()\nRUN_ANTLR4_PYTHON3(Answer.g4 IN template.stg)\nPY_SRCS(AnswerParser.py)\nEND()\n")
        self.write("template.stg", "template text\n")
        generated = self.node(self.graph(), "AnswerParser.py")
        self.assertIn(f"$(S)/{PROJECT}/template.stg", generated["inputs"])
        args = [arg for command in generated["cmds"] for arg in command["cmd_args"]]
        self.assertNotIn("IN", args)

    def test_repeated_and_empty_output_includes_keep_one_header_dependency(self):
        self.grammar()
        self.write("dependency.h", "#pragma once\n")
        self.write("ya.make", f'LIBRARY()\nADDINCL({PROJECT})\nRUN_ANTLR4(Answer.g4 -Dlanguage=Cpp -o ${{BINDIR}} IN Answer.g4 OUT AnswerParser.cpp OUTPUT_INCLUDES dependency.h dependency.h "")\nEND()\n')
        compiled = self.node(self.graph(), "AnswerParser.cpp.o")
        self.assertEqual(compiled["inputs"].count(f"$(S)/{PROJECT}/dependency.h"), 1)

    def test_general_antlr_can_declare_cpp_outputs_outside_the_module(self):
        self.grammar()
        self.write("ya.make", "LIBRARY()\nRUN_ANTLR4(Answer.g4 -o ${BINDIR}/../__ay_e2e_generated__ IN Answer.g4 OUT ${BINDIR}/../__ay_e2e_generated__/AnswerParser.cpp)\nEND()\n")
        graph = self.graph()
        output = "$(B)/__ay_e2e_generated__/AnswerParser.cpp"
        producers = [node for node in graph["graph"] if output in node["outputs"]]
        self.assertEqual(len(producers), 1)
        self.assertIn(f"$(S)/{PROJECT}/Answer.g4", producers[0]["inputs"])
