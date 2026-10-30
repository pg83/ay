from tst.make.lib import MakeTest, PROJECT


class RootedConfigurationValuesTest(MakeTest):
    def test_rooted_configuration_paths_survive_empty_appends_and_flag_binding(self):
        config = self.root / "build/ymake.core.conf"
        self.write_tree("build/ymake.core.conf", config.read_text() + f'''
E2E_CONFIG_PATH = $(S)/{PROJECT}/configured
E2E_CONFIG_PATH +=
macro E2E_ROOTED_VALUES() {{
    CFLAGS(-DE2E_CONFIG_PATH=$E2E_CONFIG_PATH -DE2E_FLAG_PATH=$E2E_FLAG_PATH)
}}
''')
        self.write("ya.make", "LIBRARY()\nSRCS(answer.cpp)\nE2E_ROOTED_VALUES()\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        compiled = self.node(self.graph(f"-DE2E_FLAG_PATH=$(S)/{PROJECT}/flag"), "answer.cpp.o")
        args = [arg.replace("$(SOURCE_ROOT)", "$(S)") for command in compiled["cmds"] for arg in command["cmd_args"]]
        self.assertIn(f"-DE2E_CONFIG_PATH=$(S)/{PROJECT}/configured", args)
        self.assertIn(f"-DE2E_FLAG_PATH=$(S)/{PROJECT}/flag", args)
