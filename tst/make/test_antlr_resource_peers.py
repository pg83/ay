import unittest

from tst import lib
from tst.make.test_codegen_antlr import antlr_files
from tst.make.test_link_test_nodes import library


def resource_names(node):
    args = [a for cmd in node["cmds"] for a in cmd["cmd_args"]]
    return [args[i + 1].split("::")[0] for i, a in enumerate(args[:-1]) if a == "--global-resource"]


def fixture(body):
    files = antlr_files(body, {
        "mod/Calc.g": "",
        "mod/Calc.g4": "grammar Calc;\n",
        "mod/CalcLexer.g4": "lexer grammar CalcLexer;\n",
        "mod/CalcParser.g4": "parser grammar CalcParser;\n",
    })
    files["build/platform/java/jdk/ya.make"] = (
        "RESOURCES_LIBRARY()\nIF(JDK_REAL_VERSION == \"17\")\n"
        "DECLARE_EXTERNAL_RESOURCE(JDK_DEFAULT sbr:31 WITH_JDK sbr:32)\nENDIF()\nEND()\n"
    )
    files["check/ya.make"] = f"UNITTEST()\n{lib.BARE_MODULE}SRCS(check.cpp)\nPEERDIR(mod)\nEND()\n"
    files["check/check.cpp"] = '#include "mod/CalcLexer.h"\n'
    files["library/cpp/testing/unittest_main/ya.make"] = library()
    return files


class AntlrResourcePeersTest(unittest.TestCase):
    def test_all_antlr_wrappers_export_default_and_versioned_jdk_resources(self):
        for declaration in (
            "RUN_ANTLR(Calc.g IN Calc.g OUT CalcLexer.cpp CalcLexer.h)\n",
            "RUN_ANTLR4(Calc.g4 IN Calc.g4 OUT CalcLexer.cpp CalcLexer.h)\n",
            "RUN_ANTLR4_CPP(Calc.g4)\n",
            "RUN_ANTLR4_CPP_SPLIT(CalcLexer.g4 CalcParser.g4)\n",
        ):
            for targets in (("check",), ("mod", "check")):
                with self.subTest(declaration=declaration, targets=targets):
                    graph = lib.make(fixture(declaration), *targets, "-t", "--sandboxing")
                    run = lib.node_by_output(graph, "$(B)/check/test-results/unittest/meta.json")
                    for name in ("JDK_DEFAULT_RESOURCE_GLOBAL", "WITH_JDK_RESOURCE_GLOBAL", "JDK17_RESOURCE_GLOBAL"):
                        self.assertEqual(resource_names(run).count(name), 1)
                    generated = lib.node_by_output(graph, "$(B)/mod/CalcLexer.cpp")
                    compiled = lib.node_by_output(graph, "$(B)/check/check.cpp.o")
                    self.assertIn(generated["uid"], compiled["deps"])
                    self.assertIn("$(B)/mod/CalcLexer.h", compiled["inputs"])

    def test_ordinary_native_peer_does_not_gain_jdk_resources(self):
        files = fixture("SRCS(ordinary.cpp)\n")
        files["mod/ordinary.cpp"] = "int ordinary;\n"
        files["check/check.cpp"] = "int main() {}\n"
        graph = lib.make(files, "check", "-t", "--sandboxing")
        run = lib.node_by_output(graph, "$(B)/check/test-results/unittest/meta.json")
        self.assertFalse(any(name.startswith(("JDK", "WITH_JDK")) for name in resource_names(run)))


if __name__ == "__main__":
    unittest.main(verbosity=2)
