from tst.make.lib import MakeTest, PROJECT


class DeclarationOrderTest(MakeTest):
    def archive_objects(self, archive):
        return [arg.replace("$(BUILD_ROOT)", "$(B)") for command in self.node(self.graph(), archive)["cmds"] for arg in command["cmd_args"] if arg.endswith(".o")]

    def test_generated_sources_keep_declaration_order_across_macros(self):
        self.write("ya.make", "LIBRARY(answer)\nCOPY_FILE(first.in first.cpp AUTO)\nPIRE_INLINE(second.cpp)\nCOPY_FILE(third.in third.cpp AUTO)\nPIRE_INLINE(fourth.cpp)\nEND()\n")
        for name in ("first.in", "second.cpp", "third.in", "fourth.cpp"):
            self.write(name, f"int {name.split('.')[0]}() {{ return 42; }}\n")
        self.assertEqual(self.archive_objects("libanswer.a"), [f"$(B)/{PROJECT}/{name}.cpp.o" for name in ("first", "second", "third", "fourth")])

    def test_python_cython_sources_keep_declaration_order_with_srcs(self):
        self.write("first.pyx", "def first():\n    return 1\n")
        self.write("second.pyx", "def second():\n    return 2\n")
        for statements, order in (("PY_SRCS(first.pyx)\nSRCS(second.pyx)\n", ("first", "second")), ("SRCS(second.pyx)\nPY_SRCS(first.pyx)\n", ("second", "first"))):
            self.write("ya.make", f"PY3_LIBRARY()\n{statements}END()\n")
            self.assertEqual(self.archive_objects(f"libpy3{PROJECT}.a"), [f"$(B)/{PROJECT}/{name}.pyx.cpp.o" for name in order], statements)
