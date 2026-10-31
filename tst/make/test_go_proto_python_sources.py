from tst.make.lib import MakeTest, PROJECT


class GoProtoPythonSourcesTest(MakeTest):
    def test_go_proto_setting_keeps_explicit_native_program_python_sources(self):
        self.write("ya.make", "PROGRAM(answer)\nUSE_PYTHON3()\nSET(GO_PROTO yes)\nPY_SRCS(helper.py)\nSRCS(main.cpp)\nEND()\n")
        self.write("main.cpp", "int main() { return 0; }\n")
        self.write("helper.py", "VALUE = 42\n")
        graph = self.graph()
        executable = self.node(graph, "answer")
        self.assertIn(self.node(graph, "main.cpp.o")["uid"], executable["deps"])
        self.assertTrue(any(f"$(S)/{PROJECT}/helper.py" in node["inputs"] for node in graph["graph"]))
