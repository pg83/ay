from tst.make.lib import MakeTest


class ConfiguredCompilerFlagsTest(MakeTest):
    def fixture(self, assignments):
        script = (self.root / "build/ymake_conf.py").read_text()
        self.write_tree("build/ymake_conf.py", script + "\n" + "\n".join(f"print({value!r})" for value in assignments) + "\n")
        self.write("ya.make", "LIBRARY()\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")

    def test_configured_compiler_flags_expand_declared_variables(self):
        self.fixture(("E2E_CUSTOM_COMPILER_FLAG=-DE2E_CUSTOM_FLAG=42", "_C_FLAGS=$E2E_CUSTOM_COMPILER_FLAG"))
        compiled = self.node(self.graph(), "answer.cpp.o")
        args = [arg for command in compiled["cmds"] for arg in command["cmd_args"]]
        self.assertIn("-DE2E_CUSTOM_FLAG=42", args)

    def test_size_optimized_clang_configuration_uses_oz(self):
        self.fixture(("BUILD_RELEASE=yes", "BUILD_SIZE_OPTIMIZED=yes", "CLANG=yes", "_C_FLAGS=$OPTIMIZE"))
        compiled = self.node(self.graph("--build=minsizerel"), "answer.cpp.o")
        args = [arg for command in compiled["cmds"] for arg in command["cmd_args"]]
        self.assertIn("-Oz", args)

    def test_debug_configuration_does_not_add_release_optimization(self):
        self.fixture(("BUILD_RELEASE=no", "_C_FLAGS=$OPTIMIZE"))
        compiled = self.node(self.graph("--build=debug"), "answer.cpp.o")
        args = [arg for command in compiled["cmds"] for arg in command["cmd_args"]]
        self.assertNotIn("-O3", args)
        self.assertNotIn("-Oz", args)

    def test_size_optimized_non_clang_configuration_uses_os(self):
        self.fixture(("BUILD_RELEASE=yes", "BUILD_SIZE_OPTIMIZED=yes", "CLANG=no", "_C_FLAGS=$OPTIMIZE"))
        compiled = self.node(self.graph("--build=minsizerel"), "answer.cpp.o")
        args = [arg for command in compiled["cmds"] for arg in command["cmd_args"]]
        self.assertIn("-Os", args)
