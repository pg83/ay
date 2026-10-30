from tst.make.lib import MakeTest


class PlatformInstructionSetFlagsTest(MakeTest):
    def test_platform_flags_disable_sse4_features(self):
        self.write("ya.make", "LIBRARY()\nCFLAGS(-DE2E_SSE4=${USE_SSE4})\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        for name in ("DISABLE_INSTRUCTION_SETS", "NOSSE"):
            with self.subTest(flag=name):
                node = self.node(self.graph("-D" + name + "=yes"), "answer.cpp.o")
                args = [arg for command in node["cmds"] for arg in command["cmd_args"]]
                self.assertIn("-DE2E_SSE4=no", args)
