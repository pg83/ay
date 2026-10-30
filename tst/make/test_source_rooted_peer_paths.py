from tst.make.lib import MakeTest


class SourceRootedPeerPathsTest(MakeTest):
    def test_source_rooted_peer_paths_keep_the_peer_archive(self):
        self.write("ya.make", f"PROGRAM(answer)\nSRCS(main.cpp)\nPEERDIR(${{ARCADIA_ROOT}}/{self.project_name}/peer)\nEND()\n")
        self.write("main.cpp", "int main() { return 0; }\n")
        self.write("peer/ya.make", "LIBRARY(peer)\nSRCS(value.cpp)\nEND()\n")
        self.write("peer/value.cpp", "int answer() { return 42; }\n")
        graph = self.graph()
        peer = self.node(graph, "peer/libpeer.a")
        self.assertIn(peer["uid"], self.node(graph, "answer")["deps"])
