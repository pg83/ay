from tst.make.lib import MakeTest
from tst.make.platform_fixtures import add_linux_clang_target


class I386ToolchainTargetsTest(MakeTest):
    def test_i386_targets_compile_libraries_with_a_configured_clang_toolchain(self):
        add_linux_clang_target(self, "i386")
        self.write("ya.make", "LIBRARY()\nNO_UTIL()\nNO_RUNTIME()\nSET(NEED_PLATFORM_PEERDIRS no)\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        compiled = self.node(self.graph("--target-platform=default-linux-i386", "-DOS_SDK=local"), "answer.cpp.o")
        self.assertIn(f"$(S)/{self.project_name}/answer.cpp", compiled["inputs"])
