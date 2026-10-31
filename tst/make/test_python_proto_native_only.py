from tst.make.lib import MakeTest, PROJECT


class PythonProtoNativeOnlyTest(MakeTest):
    def test_python_proto_without_schema_sources_keeps_its_native_archive(self):
        self.write("ya.make", "PROTO_LIBRARY(answer)\nONLY_TAGS(PY3_PROTO)\nSRC(native.cpp)\nEND()\n")
        self.write("native.cpp", "int answer() { return 42; }\n")
        graph = self.graph()
        compiled = self.node(graph, "native.cpp.py3.o")
        self.assertIn(f"$(S)/{PROJECT}/native.cpp", compiled["inputs"])
        archives = [node for node in graph["graph"] if any(out.startswith(f"$(B)/{PROJECT}/") and out.endswith(".a") for out in node["outputs"])]
        self.assertTrue(any(compiled["uid"] in node["deps"] for node in archives))
