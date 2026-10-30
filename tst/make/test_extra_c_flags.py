from tst.make.lib import MakeTest


class ExtraCFlagsTest(MakeTest):
    def flags(self, value):
        self.write("ya.make", "LIBRARY()\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        self.compiler_flags = ("-DEXTRA_C_FLAGS=" + value,)

    def command_args(self):
        return [arg for command in self.node(self.graph(*self.compiler_flags), "answer.cpp.o")["cmds"] for arg in command["cmd_args"]]

    def test_literal_modifiers_in_extra_compiler_flags(self):
        self.flags('-DE2E_QUOTED=${quo:"a b"} -DE2E_COMMA=${comma:"ignored"} -DE2E_LITERAL=${"text"}')
        args = self.command_args()
        for flag in ("-DE2E_QUOTED=a b", "-DE2E_COMMA=,", "-DE2E_LITERAL=text"):
            self.assertIn(flag, args)

    def test_quoted_extra_flags_preserve_non_special_backslashes(self):
        self.flags(r'-DE2E_PATH="folder\q" -DE2E_QUOTE=${quo:"a\"b"}')
        args = self.command_args()
        self.assertIn(r"-DE2E_PATH=folder\q", args)
        self.assertIn('-DE2E_QUOTE=a"b', args)

    def test_extra_flags_reject_incomplete_literal_references(self):
        for value in ('-DE2E=${}', '-DE2E=${quo:"unfinished}', '-DE2E=${"unfinished}', '-DE2E=${quo:"value"'):
            with self.subTest(value=value):
                self.flags(value)
                self.assertNotEqual(self.make(*self.compiler_flags).returncode, 0)

    def test_trailing_escapes_in_extra_flags_do_not_abort_graph_generation(self):
        self.flags("-DE2E_END=tail\\")
        self.assertIn("-DE2E_END=tail", self.command_args())

    def test_recursive_extra_flags_are_configuration_errors(self):
        self.flags("-DE2E_LOOP=$EXTRA_C_FLAGS")
        self.assertNotEqual(self.make(*self.compiler_flags).returncode, 0)
