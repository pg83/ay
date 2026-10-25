from tst.make.lib import MakeTest, PROJECT


class RareGeneratorsTest(MakeTest):
    def test_internal_custom_linter_is_rejected(self):
        self.write("ya.make", f"LIBRARY()\nSRCS(answer.cpp)\n_ADD_CUSTOM_EXPLICIT_LINTER_CHECK(NAME e2e WRAPPER_SCRIPT {PROJECT}/lint.py FILES ${{ARCADIA_ROOT}}/{PROJECT}/answer.cpp FILE_PROCESSING_TIME 1 EXTRA_PARAMS mode=strict)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        self.write("lint.py", "import sys\nraise SystemExit(0)\n")
        self.assertNotEqual(self.make("-t").returncode, 0)

    def test_cuda_device_sources(self):
        self.write("ya.make", "CUDA_DEVICE_LINK_LIBRARY()\nSRCS(answer.cu)\nNVCC_DEVICE_LINK(answer.cu)\nEND()\n")
        self.write("answer.cu", "__global__ void answer(int* value) { *value = 42; }\n")
        self.node(self.graph(), "devlink.o")

    def test_antlr_split_grammar(self):
        self.write("ya.make", "LIBRARY()\nRUN_ANTLR4_CPP_SPLIT(AnswerLexer.g4 AnswerParser.g4 VISITOR LISTENER)\nEND()\n")
        self.write("AnswerLexer.g4", "lexer grammar AnswerLexer;\nNUMBER: [0-9]+;\n")
        self.write("AnswerParser.g4", "parser grammar AnswerParser;\noptions { tokenVocab=AnswerLexer; }\nanswer: NUMBER EOF;\n")
        node = self.node(self.graph(), "AnswerParser.cpp")
        self.assertIn(f"$(B)/{PROJECT}/AnswerParserListener.h", node["outputs"])
        self.assertIn(f"$(B)/{PROJECT}/AnswerParserBaseVisitor.h", node["outputs"])

    def test_enum_noexcept(self):
        self.write("ya.make", f"LIBRARY()\nADDINCL({PROJECT} ${{ARCADIA_BUILD_ROOT}}/{PROJECT})\nALICE_GENERATE_NOEXCEPT_ENUM_SERIALIZATION(value.h)\nSRCS(answer.cpp)\nEND()\n")
        self.write("value.h", "#pragma once\nenum class Value { Answer = 42 };\n")
        self.write("answer.cpp", '#include "value.h_serialized.h"\nint answer() { return 42; }\n')
        self.node(self.graph(), "value.h_serialized.h")

    def test_assembly_lowercase_extension(self):
        self.write("ya.make", "LIBRARY()\nSRCS(answer.s)\nEND()\n")
        self.write("answer.s", ".text\n.globl answer\nanswer:\nmov $42, %eax\nret\n")
        graph = self.graph()
        self.assertTrue(any(f"$(S)/{PROJECT}/answer.s" in node["inputs"] for node in graph["graph"]))

    def test_generated_header_consumers(self):
        for source, output in (("answer.sc", "answer.sc.h"), ("answer.fml", "answer.fml.inc"), ("answer.inc.sfdl", "answer.inc")):
            with self.subTest(source=source):
                self.write("ya.make", f"LIBRARY()\nADDINCL({PROJECT} ${{ARCADIA_BUILD_ROOT}}/{PROJECT})\nSRCS({source} main.cpp)\nEND()\n")
                self.write(source, "\n")
                self.write("main.cpp", f'#include "{output}"\nint answer() {{ return 42; }}\n')
                self.node(self.graph(), output)
