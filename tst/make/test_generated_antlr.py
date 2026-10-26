from tst.make.lib import MakeTest, PROJECT


class GeneratedAntlrTest(MakeTest):
    def test_generated_grammar_uses_registry_producer(self):
        for extension in ("g", "g4"):
            with self.subTest(extension=extension):
                self.write("ya.make", f"LIBRARY()\nCONFIGURE_FILE(Answer.g.in ${{BINDIR}}/Answer.{extension})\nRUN_ANTLR4_CPP(${{BINDIR}}/Answer.{extension})\nEND()\n")
                self.write("Answer.g.in", "grammar Answer; answer: NUMBER EOF; NUMBER: [0-9]+; WS: [ \\	\\r\\n]+ -> skip;\n")
                graph = self.graph()
                producer = self.node(graph, f"Answer.{extension}")
                parser = self.node(graph, "AnswerParser.cpp")
                self.assertIn(f"$(B)/{PROJECT}/Answer.{extension}", parser["inputs"])
                self.assertIn(producer["uid"], parser["deps"])

    def test_generator_options_forward_input_dependencies(self):
        self.write("Answer.g4", "grammar Answer; answer: NUMBER EOF; NUMBER: [0-9]+;\n")
        self.write("Lexer.g4", "lexer grammar Lexer; NUMBER: [0-9]+;\n")
        self.write("Parser.g4", "parser grammar Parser; options { tokenVocab=Lexer; } answer: NUMBER EOF;\n")
        self.write("template.stg.in", "template text\n")
        for macro, grammar, output in (("RUN_ANTLR4_CPP", "Answer.g4", "AnswerParser.cpp"), ("RUN_ANTLR4_CPP_SPLIT", "Lexer.g4 Parser.g4", "Parser.cpp")):
            with self.subTest(macro=macro):
                self.write("ya.make", f"LIBRARY()\nCONFIGURE_FILE(template.stg.in ${{BINDIR}}/template.stg)\n{macro}({grammar} IN ${{BINDIR}}/template.stg)\nEND()\n")
                graph = self.graph()
                producer = self.node(graph, "template.stg")
                parser = self.node(graph, output)
                self.assertIn(f"$(B)/{PROJECT}/template.stg", parser["inputs"])
                self.assertIn(producer["uid"], parser["deps"])
                args = [arg for command in parser["cmds"] for arg in command["cmd_args"]]
                self.assertNotIn("IN", args)
