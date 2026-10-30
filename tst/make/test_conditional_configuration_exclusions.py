from tst.make.lib import MakeTest


class ConditionalConfigurationExclusionsTest(MakeTest):
    def test_conditional_exclusions_leave_the_selected_value_unchanged(self):
        core = self.root / "build/ymake.core.conf"
        self.write_tree("build/ymake.core.conf", core.read_text() + '''
E2E_WORDS = omitkeepomit
when ($OS_LINUX == "yes") {
    E2E_WORDS -= omit
}
macro E2E_CONDITIONAL_EXCLUSION() {
    CFLAGS(-DE2E_WORDS=$E2E_WORDS)
}
''')
        self.write("ya.make", "LIBRARY()\nE2E_CONDITIONAL_EXCLUSION()\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        node = self.node(self.graph(), "answer.cpp.o")
        args = [arg for command in node["cmds"] for arg in command["cmd_args"]]
        self.assertIn("-DE2E_WORDS=omitkeepomit", args)
