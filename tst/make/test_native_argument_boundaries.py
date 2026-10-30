from tst.make.lib import MakeTest, PROJECT


class NativeArgumentBoundariesTest(MakeTest):
    def source(self):
        self.write("answer.cpp", "int answer() { return 42; }\n")

    def test_global_keyword_is_rejected_in_peerdirs(self):
        self.source()
        self.write("ya.make", f"LIBRARY()\nPEERDIR(GLOBAL {PROJECT}/peer)\nSRCS(answer.cpp)\nEND()\n")
        self.write("peer/ya.make", "LIBRARY()\nEND()\n")
        self.assertNotEqual(self.make().returncode, 0)

    def test_unbound_resource_directory_is_rejected_in_peerdirs(self):
        self.source()
        self.write("ya.make", "LIBRARY()\nPEERDIR(${E2E_RESOURCE_GLOBAL}/peer)\nSRCS(answer.cpp)\nEND()\n")
        self.assertNotEqual(self.make().returncode, 0)
