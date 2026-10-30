from tst.make.lib import MakeTest


class SourceRootPeerValidationTest(MakeTest):
    def test_peer_paths_cannot_normalize_to_the_moduleless_source_root(self):
        self.write("ya.make", f"LIBRARY()\nPEERDIR({self.project_name}/..)\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        self.assertNotEqual(self.make().returncode, 0)
