from tst.make.lib import MakeTest, PROJECT


class SharedPythonProtoPluginsTest(MakeTest):
    def shared_schema(self, extra=""):
        self.write("ya.make", "RECURSE(first second)\n")
        self.write("answer.proto", 'syntax = "proto3"; message Answer { int32 value = 1; } service Answers { rpc Get(Answer) returns (Answer); }\n')
        self.write("first/ya.make", f"PY3_LIBRARY(first)\nPY_SRCS(${{ARCADIA_ROOT}}/{PROJECT}/answer.proto)\nEND()\n")
        self.write("second/ya.make", f"PY3_LIBRARY(second)\nGRPC()\n{extra}PY_SRCS(${{ARCADIA_ROOT}}/{PROJECT}/answer.proto)\nEND()\n")

    def test_shared_schema_keeps_extra_plugin_outputs(self):
        self.shared_schema()
        graph = self.graph()
        generated = self.node(graph, "answer__intpy3___pb2_grpc.py")
        self.assertIn(f"$(S)/{PROJECT}/answer.proto", generated["inputs"])
        compiled = [node for node in graph["graph"] if f"$(B)/{PROJECT}/answer__intpy3___pb2_grpc.py" in node["inputs"] and any(output.endswith(".yapyc3") for output in node["outputs"])]
        self.assertEqual(len(compiled), 1)
        self.assertIn(generated["uid"], compiled[0]["deps"])
