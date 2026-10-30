from tst.make.lib import MakeTest, PROJECT


class PythonProtoNativeSourcesTest(MakeTest):
    def test_python_proto_variants_keep_native_global_sources(self):
        self.write("ya.make", "PROTO_LIBRARY()\nONLY_TAGS(PY3_PROTO)\nGLOBAL_SRCS(native.cpp)\nSRCS(answer.proto)\nEND()\n")
        self.write("native.cpp", "int native_answer() { return 42; }\n")
        self.write("answer.proto", 'syntax = "proto2";\nmessage Answer {}\n')
        graph = self.graph()
        compiled = [node for node in graph["graph"] if f"$(S)/{PROJECT}/native.cpp" in node["inputs"] and any(path.endswith(".o") for path in node["outputs"])]
        self.assertEqual(len(compiled), 1)
        objects = set(compiled[0]["outputs"])
        archives = [node for node in graph["graph"] if any(path.endswith(".global.a") and f"/{PROJECT}/" in path for path in node["outputs"])]
        self.assertTrue(any(objects.intersection(node["inputs"]) and compiled[0]["uid"] in node["deps"] for node in archives))
