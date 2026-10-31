from tst.make.lib import MakeTest


class YasmI386ConfigurationTest(MakeTest):
    def test_module_architecture_switch_keeps_toolchain_assembler_values(self):
        self.write("ya.make", "LIBRARY()\nNO_UTIL()\nNO_RUNTIME()\nSET(NEED_PLATFORM_PEERDIRS no)\nSET(OS_WINDOWS yes)\nSET(ARCH_X86_64 no)\nSET(ARCH_I386 yes)\nSRCS(answer.asm)\nEND()\n")
        self.write("answer.asm", "section .text\nglobal answer\nanswer:\nret\n")
        node = self.node(self.graph(), "answer.o")
        args = [arg for command in node["cmds"] for arg in command["cmd_args"]]
        self.assertIn("WIN64", args)
        self.assertIn("win64", args)
        self.assertIn("_x86_64_", args)
