from tst.make.lib import MakeTest, PROJECT


class SharedPeerStyleSuitesTest(MakeTest):
    def test_repeated_roots_and_native_pic_peers_share_one_style_suite(self):
        self.write("ya.make", "RECURSE(program dynamic shared shared)\n")
        self.write("program/ya.make", f"PROGRAM(answer)\nSRCS(main.cpp)\nPEERDIR({PROJECT}/shared)\nEND()\n")
        self.write("program/main.cpp", "int main() { return 0; }\n")
        self.write("dynamic/ya.make", f"DLL(answer)\nSRCS(answer.cpp)\nPEERDIR({PROJECT}/shared)\nEND()\n")
        self.write("dynamic/answer.cpp", "int answer() { return 42; }\n")
        self.write("shared/ya.make", "LIBRARY(shared)\nSRCS(shared.cpp)\nSTYLE_CPP()\nEND()\n")
        self.write("shared/shared.cpp", "int shared() { return 17; }\n")
        graph = self.graph("-t", "-DADD_PEERDIRS_GEN_TESTS=yes", "--add-modules-to-results")
        checked = self.node(graph, "shared/test-results/clang_format/meta.json")
        self.assertEqual(checked["inputs"].count(f"$(S)/{PROJECT}/shared/shared.cpp"), 1)
