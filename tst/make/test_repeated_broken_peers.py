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

    def test_keep_going_preserves_programs_with_the_same_broken_peer(self):
        self.write("ya.make", "RECURSE(first second)\n")
        for name in ("first", "second"):
            self.write(name + "/ya.make", f"PROGRAM({name})\nPEERDIR({PROJECT}/broken)\nSRCS(main.cpp)\nEND()\n")
            self.write(name + "/main.cpp", "int main() { return 0; }\n")
        self.write("broken/ya.make", "INCLUDE(missing.inc)\nLIBRARY()\nEND()\n")
        graph = self.graph("-k")
        for name in ("first", "second"):
            self.assertIn(self.node(graph, name + "/" + name)["uid"], graph["result"])

    def test_keep_going_preserves_python_siblings_with_the_same_broken_peer(self):
        self.write("ya.make", "RECURSE(first second)\n")
        for name in ("first", "second"):
            self.write(name + "/ya.make", f"PY23_LIBRARY()\nPEERDIR({PROJECT}/broken)\nPY_SRCS(value.py)\nEND()\n")
            self.write(name + "/value.py", "VALUE = 42\n")
        self.write("broken/ya.make", "INCLUDE(missing.inc)\nLIBRARY()\nEND()\n")
        inputs = {path for node in self.graph("-k")["graph"] for path in node["inputs"]}
        for name in ("first", "second"):
            self.assertIn(f"$(S)/{PROJECT}/{name}/value.py", inputs)
