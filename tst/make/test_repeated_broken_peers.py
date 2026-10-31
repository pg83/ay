from tst.make.lib import MakeTest, PROJECT


class RepeatedBrokenPeersTest(MakeTest):
    def test_keep_going_preserves_libraries_with_the_same_broken_peer(self):
        self.write("ya.make", "RECURSE(first second)\n")
        for name in ("first", "second"):
            self.write(name + "/ya.make", f"LIBRARY()\nPEERDIR({PROJECT}/broken)\nSRCS(value.cpp)\nEND()\n")
            self.write(name + "/value.cpp", "int value() { return 42; }\n")
        self.write("broken/ya.make", "INCLUDE(missing.inc)\nLIBRARY()\nEND()\n")
        graph = self.graph("-k")
        for name in ("first", "second"):
            self.node(graph, name + "/value.cpp.o")
