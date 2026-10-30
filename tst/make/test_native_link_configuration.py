from tst.make.lib import MakeTest, PROJECT


def command_args(node):
    return [arg.replace("$(SOURCE_ROOT)", "$(S)") for command in node["cmds"] for arg in command["cmd_args"]]


class NativeLinkConfigurationTest(MakeTest):
    def test_compiler_runtime_setting_controls_runtime_header_paths(self):
        self.write("ya.make", "LIBRARY()\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        for setting in ("yes", "no"):
            with self.subTest(setting=setting):
                args = command_args(self.node(self.graph("-DUSE_ARCADIA_COMPILER_RUNTIME=" + setting), "answer.cpp.o"))
                self.assertEqual(any(arg.startswith("-I$(S)/contrib/libs/clang") and arg.endswith("-rt/include") for arg in args), setting == "yes")

    def test_empty_host_runtime_setting_reports_missing_sanitizer_headers(self):
        self.write("ya.make", f"LIBRARY()\nRUN_PROGRAM({PROJECT}/tool OUT generated.cpp)\nEND()\n")
        self.write("tool/ya.make", "PROGRAM(generator)\nSRCS(main.cpp)\nEND()\n")
        self.write("tool/main.cpp", "int main() { return 0; }\n")
        result = self.make("-DUSE_ARCADIA_COMPILER_RUNTIME=yes", "--host-platform-flag=USE_ARCADIA_COMPILER_RUNTIME=")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("sanitizer/asan_interface.h", result.stderr)

    def test_library_common_link_settings_expose_arcadia_math_headers(self):
        self.write("ya.make", "LIBRARY()\nSET(COMMON_LINK_SETTINGS yes)\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        args = command_args(self.node(self.graph("-DUSE_ARCADIA_LIBM=yes"), "answer.cpp.o"))
        self.assertIn("-I$(S)/contrib/libs/libm/include", args)

    def test_autocheck_go_programs_link_the_eatmydata_library(self):
        self.write("ya.make", "GO_PROGRAM(answer)\nSRCS(main.go)\nEND()\n")
        self.write("main.go", "package main\nfunc main() {}\n")
        program = self.node(self.graph("-DAUTOCHECK=yes", "--host-platform-flag=AUTOCHECK=yes"), "answer")
        self.assertTrue(any("contrib/libs/libeatmydata/" in value for value in program["inputs"]))
