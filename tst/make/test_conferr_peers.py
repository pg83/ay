from tst.make.lib import MakeTest


class PeerConfigureErrorsTest(MakeTest):
    """Configure errors of peer, tool and directory resolution (devtools/ymake/add_iter.cpp, module_dir.cpp)."""

    def library(self, *statements):
        self.write("ya.make", "LIBRARY()\n" + "".join(statement + "\n" for statement in statements) + "END()\n")

    def tool(self, ya_make):
        self.write("tool/ya.make", ya_make)
        self.write("tool/main.cpp", "int main() { return 0; }\n")

    def run_tool(self):
        return f"RUN_PROGRAM({self.project_name}/tool STDOUT out.cpp)"

    def test_peerdir_to_a_file(self):
        self.library(f"PEERDIR({self.project_name}/value.txt)")
        self.write("value.txt", "value\n")
        self.assertConfigureLikeYa()

    def test_depends_on_a_missing_directory(self):
        self.write("ya.make", f"UNITTEST()\nSRCS(test.cpp)\nDEPENDS({self.project_name}/missing)\nEND()\n")
        self.write("test.cpp", "int answer() { return 42; }\n")
        self.assertConfigureLikeYa()

    def test_srcdir_to_a_missing_directory(self):
        self.library(f"SRCDIR({self.project_name}/missing)")
        self.assertConfigureLikeYa()

    def test_peerdir_to_a_directory_without_modules(self):
        self.library(f"PEERDIR({self.project_name}/sub)")
        self.write("sub/ya.make", "RECURSE()\n")
        self.assertConfigureLikeYa()

    def test_peerdir_to_a_program(self):
        self.library(f"PEERDIR({self.project_name}/tool)")
        self.tool("PROGRAM()\nSRCS(main.cpp)\nEND()\n")
        self.assertConfigureLikeYa()

    def test_peerdir_to_an_internal_directory(self):
        self.library(f"PEERDIR({self.project_name}/other/internal)")
        self.write("other/internal/ya.make", "LIBRARY()\nSRCS(value.cpp)\nEND()\n")
        self.write("other/internal/value.cpp", "int value() { return 42; }\n")
        self.assertConfigureLikeYa()

    def test_peerdir_from_cpp_to_go(self):
        self.library(f"PEERDIR({self.project_name}/lib)")
        self.write("lib/ya.make", "GO_LIBRARY()\nSRCS(value.go)\nEND()\n")
        self.write("lib/value.go", "package value\n")
        self.assertConfigureLikeYa()

    def test_tool_from_a_missing_directory(self):
        self.library(f"RUN_PROGRAM({self.project_name}/missing STDOUT out.cpp)")
        self.assertConfigureLikeYa()

    def test_tool_from_a_directory_without_ya_make(self):
        self.library(self.run_tool())
        self.write("tool/main.cpp", "int main() { return 0; }\n")
        self.assertConfigureLikeYa()

    def test_tool_from_a_directory_without_modules(self):
        self.library(self.run_tool())
        self.tool("RECURSE()\n")
        self.assertConfigureLikeYa()

    def test_tool_from_a_library(self):
        self.library(self.run_tool())
        self.tool("LIBRARY()\nSRCS(main.cpp)\nEND()\n")
        self.assertConfigureLikeYa()
