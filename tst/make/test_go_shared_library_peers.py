from tst.make.lib import MakeTest, PROJECT


class GoSharedLibraryPeersTest(MakeTest):
    def test_native_library_accepts_a_go_shared_library_peer(self):
        self.write("ya.make", f"LIBRARY()\nPEERDIR({PROJECT}/shared)\nSRCS(native.cpp)\nEND()\n")
        self.write("native.cpp", "int native_answer() { return 42; }\n")
        self.write("shared/ya.make", "GO_DLL(answer)\nCGO_SRCS(answer.go)\nEND()\n")
        self.write("shared/answer.go", 'package main\nimport "C"\n//export Answer\nfunc Answer() C.int { return 42 }\nfunc main() {}\n')
        self.node(self.graph(), "native.cpp.o")
