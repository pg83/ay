from tst.make.lib import MakeTest


class AntlrPythonArgumentsTest(MakeTest):
    def test_python2_generator_forwards_additional_options(self):
        self.write("ya.make", "PY2_LIBRARY()\nRUN_ANTLR4_PYTHON2(Answer.g4 -Werror)\nPY_SRCS(AnswerParser.py)\nEND()\n")
        self.write("Answer.g4", "grammar Answer; answer: NUMBER EOF; NUMBER: [0-9]+;\n")
        parser = self.node(self.graph(), "AnswerParser.py")
        args = [arg for command in parser["cmds"] for arg in command["cmd_args"]]
        self.assertIn("-Dlanguage=Python2", args)
        self.assertIn("-Werror", args)

    def test_empty_expanded_grammar_is_rejected(self):
        for version in (2, 3):
            with self.subTest(version=version):
                self.write("ya.make", f"PY{version}_LIBRARY()\nSET(E2E_EMPTY)\nRUN_ANTLR4_PYTHON{version}(${{E2E_EMPTY}})\nEND()\n")
                self.assertNotEqual(self.make().returncode, 0)

    def test_subdir_without_value_is_rejected(self):
        self.write("Answer.g4", "grammar Answer; answer: NUMBER EOF; NUMBER: [0-9]+;\n")
        for version in (2, 3):
            with self.subTest(version=version):
                self.write("ya.make", f"PY{version}_LIBRARY()\nRUN_ANTLR4_PYTHON{version}(Answer.g4 SUBDIR)\nEND()\n")
                self.assertNotEqual(self.make().returncode, 0)
