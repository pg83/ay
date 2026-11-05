from tst.make.lib import MakeTest, PROJECT


class StatementMapOrderTest(MakeTest):
    def test_generated_sources_keep_declaration_order_across_macros(self):
        self.write("ya.make", "LIBRARY(answer)\nCOPY_FILE(first.in first.cpp AUTO)\nPIRE_INLINE(second.cpp)\nCOPY_FILE(third.in third.cpp AUTO)\nPIRE_INLINE(fourth.cpp)\nEND()\n")
        for name in ("first.in", "second.cpp", "third.in", "fourth.cpp"):
            self.write(name, f"int {name.split('.')[0]}() {{ return 42; }}\n")
        objects = [arg.replace("$(BUILD_ROOT)", "$(B)") for command in self.node(self.graph(), "libanswer.a")["cmds"] for arg in command["cmd_args"] if arg.endswith(".o")]
        self.assertEqual(objects, [f"$(B)/{PROJECT}/{name}.cpp.o" for name in ("first", "second", "third", "fourth")])
