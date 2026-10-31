from tst.make.lib import MakeTest


class YasmLiteralRootFlagsTest(MakeTest):
    def test_literal_rooted_flag_values_reach_the_assembler(self):
        flag = f"$(S)/{self.project_name}/defs.inc"
        self.write("ya.make", f"LIBRARY()\nSET(YASM_FLAGS {flag})\nSRCS(answer.asm)\nEND()\n")
        self.write("answer.asm", "section .text\nglobal answer\nanswer:\nret\n")
        self.write("defs.inc", "%define VALUE 42\n")
        node = self.node(self.graph(), "answer.o")
        args = [arg.replace("$(SOURCE_ROOT)", "$(S)") for command in node["cmds"] for arg in command["cmd_args"]]
        self.assertIn(flag, args)
