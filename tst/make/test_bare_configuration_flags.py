from tst.make.lib import MakeTest


class BareConfigurationFlagsTest(MakeTest):
    def test_bare_flags_enable_module_conditions(self):
        self.write("ya.make", "LIBRARY()\nIF(E2E_ENABLED)\nCFLAGS(-DE2E_ENABLED=${E2E_ENABLED})\nSRCS(answer.cpp)\nELSE()\nMESSAGE(FATAL_ERROR flag_was_not_enabled)\nENDIF()\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        node = self.node(self.graph("-DE2E_ENABLED"), "answer.cpp.o")
        args = [arg for command in node["cmds"] for arg in command["cmd_args"]]
        self.assertIn("-DE2E_ENABLED=yes", args)
