from tst.make import toolchains
from tst.make.lib import MakeTest


class ToolchainExtraFlagsTest(MakeTest):
    def test_toolchain_flags_are_available_to_module_configuration(self):
        files = toolchains.cxx_files(self)
        for _, _, chain in toolchains.cxx_entries(files):
            chain.setdefault("params", {}).setdefault("extra_flags", {})["E2E_TOOLCHAIN_FLAG"] = "42"
        toolchains.write(self, files)
        self.write("ya.make", "LIBRARY()\nCFLAGS(-DE2E_TOOLCHAIN_FLAG=${E2E_TOOLCHAIN_FLAG})\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        node = self.node(self.graph(), "answer.cpp.o")
        args = [arg for command in node["cmds"] for arg in command["cmd_args"]]
        self.assertIn("-DE2E_TOOLCHAIN_FLAG=42", args)
