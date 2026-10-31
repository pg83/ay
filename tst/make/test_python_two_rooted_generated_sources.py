from tst.make.lib import MakeTest, PROJECT


class PythonTwoRootedGeneratedSourcesTest(MakeTest):
    def test_python_two_bytecode_keeps_a_build_rooted_generator_source(self):
        self.write("ya.make", f"PY2_LIBRARY()\nCOPY_FILE(template.py generated.py)\nPY_SRCS(${{ARCADIA_BUILD_ROOT}}/{PROJECT}/generated.py)\nEND()\n")
        self.write("template.py", "VALUE = 42\n")
        graph = self.graph()
        generated = self.node(graph, "generated.py")
        compiled = [node for node in graph["graph"] if f"$(B)/{PROJECT}/generated.py" in node["inputs"] and any(output.endswith(".yapyc") for output in node["outputs"])]
        self.assertEqual(len(compiled), 1)
        self.assertIn(generated["uid"], compiled[0]["deps"])
