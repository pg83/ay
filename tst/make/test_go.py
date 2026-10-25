from tst.make.lib import MakeTest, PROJECT


class GoTest(MakeTest):
    def test_go_library_sources(self):
        self.write("ya.make", "GO_LIBRARY()\nSRCS(answer.go)\nEND()\n")
        self.write("answer.go", "package answer\nfunc Value() int { return 42 }\n")
        graph = self.graph()
        compilation = [node for node in graph["graph"] if f"$(S)/{PROJECT}/answer.go" in node["inputs"]]
        self.assertTrue(compilation, "no command consumes the Go source")
        self.assertTrue(any(path.endswith(".a") for node in compilation for path in node["outputs"]))
