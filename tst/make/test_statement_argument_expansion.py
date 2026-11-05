from tst.make.lib import MakeTest, PROJECT


class StatementArgumentExpansionTest(MakeTest):
    def write_sources(self):
        for name in ("x.cpp", "xanswer.cpp"):
            self.write(name, "int f() { return 42; }\n")

    def test_braced_variable_takes_its_value_at_the_statement(self):
        self.write("ya.make", "LIBRARY(answer)\nSRCS(x${NAME}.cpp)\nSET(NAME answer)\nEND()\n")
        self.write_sources()
        objects = [arg.replace("$(BUILD_ROOT)", "$(B)") for command in self.node(self.graph(), "libanswer.a")["cmds"] for arg in command["cmd_args"] if arg.endswith(".o")]
        self.assertEqual(objects, [f"$(B)/{PROJECT}/x.cpp.o"])

    def test_bare_variable_stays_literal_in_statement_arguments(self):
        self.write("ya.make", "LIBRARY(answer)\nSET(NAME answer)\nSRCS(x$NAME.cpp)\nEND()\n")
        self.write_sources()
        result = self.make()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("x$NAME.cpp", result.stdout + result.stderr)
