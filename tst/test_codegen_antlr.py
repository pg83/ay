import unittest

import lib


JAVA_HEAD = [
    "$(S)/build/scripts/stdout2stderr.py", "$(B)/resources/JDK17/bin/java", "-jar",
]
ANTLR4_JAR = "$(S)/contrib/java/antlr/antlr4/antlr.jar"
ANTLR3_JAR = "$(S)/contrib/java/antlr/antlr3/antlr.jar"
RUNTIME_H = "$(S)/contrib/libs/antlr4_cpp_runtime/src/antlr4-runtime.h"


def antlr_files(body, sources):
    return {
        "mod/ya.make": "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n" + body + "END()\n",
        "mod/extra.h": "",
        "build/scripts/stdout2stderr.py": "",
        "contrib/java/antlr/antlr3/antlr.jar": "",
        "contrib/libs/antlr4_cpp_runtime/src/antlr4-runtime.h": "",
        "contrib/libs/antlr4_cpp_runtime/ya.make": (
            "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n"
        ),
        "build/platform/java/jdk/jdk17/ya.make": (
            "RESOURCES_LIBRARY()\nDECLARE_EXTERNAL_RESOURCE(JDK17 sbr:1)\nEND()\n"
        ),
        **sources,
    }


class Antlr4CppTest(unittest.TestCase):
    def test_combined_grammar_generates_copied_sources(self):
        graph = lib.make(antlr_files(
            "RUN_ANTLR4_CPP(Calc.g4 VISITOR LISTENER -package calc "
            "OUTPUT_INCLUDES mod/extra.h IN ignored OUT ignored)\n",
            {"mod/Calc.g4": "grammar Calc;\n"},
        ), "mod")

        java = lib.node_by_output(graph, "$(B)/mod/CalcLexer.cpp")
        self.assertEqual(java["kv"], {"p": "JV", "pc": "light-blue", "show_out": "yes"})
        self.assertEqual(java["outputs"], [
            "$(B)/mod/CalcLexer.cpp", "$(B)/mod/CalcLexer.h",
            "$(B)/mod/CalcParser.cpp", "$(B)/mod/CalcParser.h",
            "$(B)/mod/CalcVisitor.h", "$(B)/mod/CalcBaseVisitor.h",
        ])
        self.assertEqual(java["cmds"][0]["cmd_args"][1:], JAVA_HEAD + [
            ANTLR4_JAR, "$(S)/mod/Calc.g4", "-Dlanguage=Cpp", "-o", "$(B)/mod",
            "-visitor", "-listener", "-package", "calc",
        ])
        self.assertEqual(java["cmds"][0]["cwd"], "$(B)/mod")
        self.assertEqual(java["inputs"], [
            "$(S)/mod/Calc.g4", "$(S)/build/scripts/stdout2stderr.py", ANTLR4_JAR,
        ])

        for base in ("CalcLexer", "CalcParser"):
            copy = lib.node_by_output(graph, f"$(B)/mod/{base}.g4.cpp")
            self.assertEqual(copy["kv"], {"p": "CP", "pc": "light-cyan"})
            self.assertEqual(copy["cmds"][0]["cmd_args"][1:], [
                "$(S)/build/scripts/fs_tools.py", "copy",
                f"$(B)/mod/{base}.cpp", f"$(B)/mod/{base}.g4.cpp",
            ])
            self.assertEqual(copy["deps"], [java["uid"]])
            for path in ("$(B)/mod/CalcLexer.cpp", f"$(B)/mod/{base}.cpp",
                         "$(S)/mod/Calc.g4", "$(S)/mod/extra.h", RUNTIME_H):
                self.assertIn(path, copy["inputs"])
            compile_node = lib.node_by_output(graph, f"$(B)/mod/{base}.g4.cpp.o")
            self.assertIn("-Wno-unused-variable", compile_node["cmds"][0]["cmd_args"])
            self.assertIn(f"$(B)/mod/{base}.h", compile_node["inputs"])

    def test_split_grammar_defaults_to_no_listener(self):
        graph = lib.make(antlr_files(
            "RUN_ANTLR4_CPP_SPLIT(CalcLexer.g4 CalcParser.g4 NO_LISTENER "
            "OUTPUT_INCLUDES mod/extra.h IN ignored OUT ignored)\n",
            {"mod/CalcLexer.g4": "", "mod/CalcParser.g4": ""},
        ), "mod")
        java = lib.node_by_output(graph, "$(B)/mod/CalcParserVisitor.h")
        self.assertEqual(java["outputs"], [
            "$(B)/mod/CalcLexer.cpp", "$(B)/mod/CalcLexer.h",
            "$(B)/mod/CalcParser.cpp", "$(B)/mod/CalcParser.h",
            "$(B)/mod/CalcParserVisitor.h", "$(B)/mod/CalcParserBaseVisitor.h",
        ])
        self.assertEqual(java["cmds"][0]["cmd_args"][1:], JAVA_HEAD + [
            ANTLR4_JAR, "$(S)/mod/CalcLexer.g4", "$(S)/mod/CalcParser.g4",
            "-Dlanguage=Cpp", "-o", "$(B)/mod", "-no-listener",
        ])
        self.assertEqual(java["inputs"], [
            "$(S)/mod/CalcLexer.g4", "$(S)/mod/CalcParser.g4",
            "$(S)/build/scripts/stdout2stderr.py", ANTLR4_JAR,
        ])
        parser_copy = lib.node_by_output(graph, "$(B)/mod/CalcParser.g4.cpp")
        self.assertIn("$(S)/mod/CalcLexer.g4", parser_copy["inputs"])
        lib.node_by_output(graph, "$(B)/mod/CalcLexer.g4.cpp.o")

    def test_split_grammar_visitor_and_listener_flags(self):
        graph = lib.make(antlr_files(
            "RUN_ANTLR4_CPP_SPLIT(CalcLexer.g4 CalcParser.g4 VISITOR LISTENER)\n",
            {"mod/CalcLexer.g4": "", "mod/CalcParser.g4": ""},
        ), "mod")
        java = lib.node_by_output(graph, "$(B)/mod/CalcParserVisitor.h")
        self.assertEqual(java["cmds"][0]["cmd_args"][-2:], ["-visitor", "-listener"])


class RunAntlrTest(unittest.TestCase):
    def test_antlr3_run_substitutes_paths_and_compiles_outputs(self):
        graph = lib.make(antlr_files(
            "COPY_FILE(tpl.g Gen.g)\n"
            "SRCS(use.cpp)\n"
            "RUN_ANTLR(\n"
            "  ${CURDIR}/Calc.g ${BINDIR}/Gen.g -o ${BINDIR} -lib CalcLexer.cpp\n"
            "  IN Calc.g ${BINDIR}/Gen.g\n"
            "  OUT CalcLexer.cpp CalcLexer.h CalcParser.cpp CalcParser.h Calc.h Calc.cpp Calc.tokens\n"
            "  OUTPUT_INCLUDES mod/extra.h mod/Calc.g\n"
            "  CWD ${BINDIR}\n"
            ")\n",
            {
                "mod/Calc.g": "",
                "mod/tpl.g": "",
                "mod/use.cpp": '#include "CalcLexer.h"\n#include "Calc.h"\n',
            },
        ), "mod")
        copy = lib.node_by_output(graph, "$(B)/mod/Gen.g")
        java = lib.node_by_output(graph, "$(B)/mod/Calc.tokens")
        self.assertEqual(java["cmds"][0]["cmd_args"][1:], JAVA_HEAD + [
            ANTLR3_JAR, "$(S)/mod/Calc.g", "$(B)/mod/Gen.g",
            "-o", "$(B)/mod", "-lib", "$(B)/mod/CalcLexer.cpp",
        ])
        self.assertEqual(java["cmds"][0]["cwd"], "$(B)/mod")
        self.assertEqual(java["inputs"], [
            "$(S)/mod/Calc.g", "$(S)/mod/tpl.g", "$(B)/mod/Gen.g",
            "$(S)/build/scripts/stdout2stderr.py", ANTLR3_JAR,
        ])
        self.assertIn(copy["uid"], java["deps"])

        lexer = lib.node_by_output(graph, "$(B)/mod/CalcLexer.cpp.o")
        self.assertEqual(lexer["inputs"][:2], [
            "$(B)/mod/CalcLexer.cpp", "$(B)/mod/CalcParser.cpp",
        ])
        for path in ("$(S)/mod/extra.h", "$(S)/mod/Calc.g", "$(S)/mod/tpl.g", ANTLR3_JAR):
            self.assertIn(path, lexer["inputs"])
        parser = lib.node_by_output(graph, "$(B)/mod/CalcParser.cpp.o")
        self.assertNotIn("$(B)/mod/CalcLexer.cpp", parser["inputs"])
        lib.node_by_output(graph, "$(B)/mod/Calc.cpp.o")

        use = lib.node_by_output(graph, "$(B)/mod/use.cpp.o")
        for path in ("$(B)/mod/CalcLexer.h", "$(B)/mod/CalcParser.cpp",
                     "$(B)/mod/Calc.h", "$(B)/mod/Calc.cpp"):
            self.assertIn(path, use["inputs"])
        self.assertIn(java["uid"], use["deps"])

    def test_compiled_output_outside_module_is_rejected(self):
        with self.assertRaisesRegex(
            AssertionError, 'antlr output "other/x.cpp" is outside module "mod"',
        ):
            lib.make(antlr_files(
                "RUN_ANTLR(Calc.g IN Calc.g OUT ${ARCADIA_BUILD_ROOT}/other/x.cpp)\n",
                {"mod/Calc.g": ""},
            ), "mod")


if __name__ == "__main__":
    unittest.main(verbosity=2)
