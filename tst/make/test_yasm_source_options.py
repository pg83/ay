from tst.make.lib import MakeTest, PROJECT


class YasmSourceOptionsTest(MakeTest):
    def fixture(self, macros):
        self.write("ya.make", "LIBRARY()\n" + macros + "\nEND()\n")
        self.write("answer.asm", "section .text\nglobal answer\nanswer:\nmov eax, VALUE\nret\n")
        self.write("defs.inc", "%define VALUE 42\n")
        self.write("common.inc", "%define SHARED 1\n")

    def test_source_flags_named_preincludes_remain_plain_arguments(self):
        self.fixture("ASM_PREINCLUDE(common.inc)\nSRC(answer.asm PREINCLUDES defs.inc)")
        compiled = self.node(self.graph(), "answer.o")
        args = [arg for command in compiled["cmds"] for arg in command["cmd_args"]]
        self.assertIn(f"$(S)/{PROJECT}/common.inc", compiled["inputs"])
        self.assertIn("common.inc", args)
        self.assertNotIn(f"$(S)/{PROJECT}/defs.inc", compiled["inputs"])
        self.assertIn("PREINCLUDES", args)
        self.assertIn("defs.inc", args)

    def test_symbol_prefix_reaches_the_assembler(self):
        self.fixture("SET(ASM_PREFIX e2e_)\nSRCS(answer.asm)")
        compiled = self.node(self.graph(), "answer.o")
        args = [arg for command in compiled["cmds"] for arg in command["cmd_args"]]
        self.assertIn("--prefix=e2e_", args)

    def test_rooted_assembler_flag_values_are_expanded(self):
        self.fixture("SET(YASM_FLAGS ${ARCADIA_ROOT})\nSRCS(answer.asm)")
        compiled = self.node(self.graph(), "answer.o")
        args = [arg.replace("$(SOURCE_ROOT)", "$(S)") for command in compiled["cmds"] for arg in command["cmd_args"]]
        self.assertIn("$(S)", args)
