from tst.make.lib import MakeTest, PROJECT


class PrebuiltProgramPlatformsTest(MakeTest):
    def test_prebuilt_programs_reject_primary_outputs_that_expand_to_empty(self):
        self.write("ya.make", "PREBUILT_PROGRAM(answer)\nPRIMARY_OUTPUT(${E2E_UNDEFINED_OUTPUT})\nEND()\n")
        self.assertNotEqual(self.make().returncode, 0)

    def test_windows_yql_prebuilt_tools_use_the_exe_suffix(self):
        self.write("ya.make", "YQL_PREBUILT_TOOL(answer)\nCOPY_FILE(value.exe staged.exe)\nPRIMARY_OUTPUT(${BINDIR}/staged.exe)\nEND()\n")
        self.write("value.exe", "prebuilt executable\n")
        graph = self.graph("--target-platform=default-win-x86_64")
        copied = self.node(graph, "answer.exe")
        self.assertIn(self.node(graph, "staged.exe")["uid"], copied["deps"])

    def test_windows_prebuilt_programs_use_the_exe_suffix(self):
        self.write("ya.make", f"PREBUILT_PROGRAM(answer)\nPRIMARY_OUTPUT(${{ARCADIA_ROOT}}/{PROJECT}/value.exe)\nEND()\n")
        self.write("value.exe", "prebuilt executable\n")
        copied = self.node(self.graph("--target-platform=default-win-x86_64"), "answer.exe")
        args = [arg.replace("$(SOURCE_ROOT)", "$(S)")
                for command in copied["cmds"] for arg in command["cmd_args"]]
        self.assertIn(f"$(S)/{PROJECT}/value.exe", args)
