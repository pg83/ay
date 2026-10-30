from tst.make.lib import MakeTest, PROJECT


class PythonProtoGlobalSchemasTest(MakeTest):
    def fixture(self, source):
        self.write("ya.make", "RECURSE(schema consumer)\n")
        self.write("schema/ya.make", f"PROTO_LIBRARY()\nOPTIMIZE_PY_PROTOS()\nGLOBAL_SRCS({source})\nEND()\n")
        self.write(f"schema/{source}", 'syntax = "proto2";\nmessage Answer { optional int32 value = 1; }\n')
        self.write("consumer/ya.make", f"PY3_PROGRAM(answer)\nPY_SRCS(MAIN main.py)\nPEERDIR({PROJECT}/schema)\nEND()\n")
        self.write("consumer/main.py", "print(42)\n")

    def check_native_python_object(self, graph, source):
        generated = self.node(graph, "schema/" + source)
        compiled = [node for node in graph["graph"] if f"$(B)/{PROJECT}/schema/{source}" in node["inputs"] and any(out.endswith((".py3.o", ".py3.pic.o")) for out in node["outputs"])]
        self.assertTrue(compiled)
        self.assertTrue(any(generated["uid"] in node["deps"] for node in compiled))
        self.node(graph, "consumer/answer")

    def test_global_gazetteer_schemas_require_their_runtime_dependency(self):
        self.fixture("answer.gztproto")
        result = self.make()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("kernel/gazetteer/proto", result.stderr)

    def test_global_event_schemas_keep_native_python_compilation(self):
        self.fixture("answer.ev")
        self.check_native_python_object(self.graph(), "answer.ev.pb.cc")
