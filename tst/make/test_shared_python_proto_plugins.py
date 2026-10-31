from tst.make.lib import MakeTest, PROJECT


class SharedPythonProtoPluginsTest(MakeTest):
    def shared_schema(self, extra="", schema=f"{PROJECT}/answer.proto"):
        self.write("ya.make", "RECURSE(first second)\n")
        self.write("answer.proto", 'syntax = "proto3"; message Answer { int32 value = 1; } service Answers { rpc Get(Answer) returns (Answer); }\n')
        self.write("first/ya.make", f"PY3_LIBRARY(first)\nPY_SRCS(${{ARCADIA_ROOT}}/{schema})\nEND()\n")
        self.write("second/ya.make", f"PY3_LIBRARY(second)\nGRPC()\n{extra}PY_SRCS(${{ARCADIA_ROOT}}/{schema})\nEND()\n")

    def test_shared_schema_keeps_extra_plugin_outputs(self):
        self.shared_schema()
        graph = self.graph()
        generated = self.node(graph, "answer__intpy3___pb2_grpc.py")
        self.assertIn(f"$(S)/{PROJECT}/answer.proto", generated["inputs"])
        compiled = [node for node in graph["graph"] if f"$(B)/{PROJECT}/answer__intpy3___pb2_grpc.py" in node["inputs"] and any(output.endswith(".yapyc3") for output in node["outputs"])]
        self.assertEqual(len(compiled), 1)
        self.assertIn(generated["uid"], compiled[0]["deps"])

    def test_preexisting_bytecode_does_not_hide_extra_plugin_sources(self):
        schema = "__ay_root_e2e__.proto"
        output = "__ay_root_e2e____intpy3___pb2_grpc.py"
        self.write_tree(schema, 'syntax = "proto3"; message Answer {}\n')
        self.shared_schema(f"COPY_FILE(seed.txt ${{ARCADIA_BUILD_ROOT}}/{output}.yapyc3)\n", schema)
        self.write("second/seed.txt", "preexisting bytecode\n")
        graph = self.graph()
        generated = [node for node in graph["graph"] if f"$(B)/{output}" in node["outputs"]]
        self.assertEqual(len(generated), 1)
        self.assertIn(f"$(S)/{schema}", generated[0]["inputs"])
