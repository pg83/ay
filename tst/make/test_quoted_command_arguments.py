from tst.make.lib import MakeTest, PROJECT


class QuotedCommandArgumentsTest(MakeTest):
    def flex(self, value):
        self.write("ya.make", "LIBRARY()\nSRCS(lexer.l)\nEND()\n")
        self.write("lexer.l", "%option noyywrap\n%%\n. ;\n%%\n")
        self.write_tree("local.ymake", "LEX_FLAGS = " + value + "\n")
        generated = self.node(self.graph(), "lexer.l.cpp")
        return [arg for command in generated["cmds"] for arg in command["cmd_args"]]

    def test_flex_flags_keep_escaped_spaces_in_one_argument(self):
        args = self.flex(r"--prefix=left\ right")
        self.assertIn("--prefix=left right", args)

    def test_flex_flags_ignore_a_trailing_escape(self):
        args = self.flex("--prefix=answer\\")
        self.assertIn("--prefix=answer", args)

    def test_java_generator_preserves_quoted_backslashes(self):
        self.write("tool/ya.make", "JAVA_PROGRAM(generator)\nJDK_VERSION(17)\nJAVA_SRCS(Generator.java)\nEND()\n")
        self.write("tool/Generator.java", "public class Generator { public static void main(String[] args) {} }\n")
        arguments = r'"left\q" "left\\right" "left\/right"'
        self.write("ya.make", f"JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nRUN_JAVA_PROGRAM(Generator ${{E2E_GENERATOR_ARGS}} CLASSPATH {PROJECT}/tool OUT Generated.java)\nJAVA_SRCS(Generated.java)\nEND()\n")
        generated = self.node(self.graph("-DE2E_GENERATOR_ARGS=" + arguments), "Generated.java")
        args = next(command["cmd_args"] for command in generated["cmds"] if "Generator" in command["cmd_args"])
        self.assertEqual(args[args.index("Generator") + 1:], [r"left\q", r"left\right", "left/right"])

    def test_configure_file_preserves_escaped_surrounding_quotes(self):
        self.write("ya.make", "LIBRARY()\nSET(E2E_VALUE ${E2E_QUOTED})\nCONFIGURE_FILE(config.h.in ${BINDIR}/config.h)\nSRCS(answer.cpp)\nEND()\n")
        self.write("config.h.in", "#define ANSWER @E2E_VALUE@\n")
        self.write("answer.cpp", '#include "config.h"\nint answer() { return ANSWER; }\n')
        generated = self.node(self.graph(r'-DE2E_QUOTED=\"42\"'), "config.h")
        args = [arg for command in generated["cmds"] for arg in command["cmd_args"]]
        self.assertIn('E2E_VALUE="42"', args)
