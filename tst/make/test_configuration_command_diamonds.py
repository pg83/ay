from tst.make.lib import MakeTest


class ConfigurationCommandDiamondsTest(MakeTest):
    def test_shared_command_variable_dependencies_expand_in_each_branch(self):
        core = self.root / "build/ymake.core.conf"
        self.write_tree("build/ymake.core.conf", core.read_text() + """
E2E_SHARED_FLAG = -DE2E_SHARED=42
E2E_LEFT_FLAG = $E2E_SHARED_FLAG
E2E_RIGHT_FLAG = $E2E_SHARED_FLAG
when ($OS_LINUX == "yes") {
    PM_SCRIPT = $E2E_LEFT_FLAG $E2E_RIGHT_FLAG
}
macro E2E_COMMAND_DIAMOND() {
    CFLAGS($PM_SCRIPT)
}
""")
        self.write("ya.make", "LIBRARY()\nE2E_COMMAND_DIAMOND()\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "int answer() { return E2E_SHARED; }\n")
        node = self.node(self.graph(), "answer.cpp.o")
        args = [arg for command in node["cmds"] for arg in command["cmd_args"]]
        self.assertIn("-DE2E_SHARED=42", args)
