from tst.make.lib import MakeTest, PROJECT


class ExplicitRootProtoPeersTest(MakeTest):
    def test_python_variants_keep_an_explicit_native_proto_root(self):
        self.write("ya.make", "RECURSE(schema consumer)\n")
        self.write("schema/ya.make", "PROTO_LIBRARY(schema)\nONLY_TAGS(CPP_PROTO)\nSRCS(answer.proto)\nEND()\n")
        self.write("schema/answer.proto", 'syntax = "proto3"; message Answer { int32 value = 1; }\n')
        self.write("consumer/ya.make", f"PY23_LIBRARY(consumer)\nSET(PEERDIR_TAGS CPP_PROTO __EMPTY__ RESOURCE_LIB DLL_LIB)\nPY_SRCS(answer.py)\nPEERDIR({PROJECT}/schema)\nEND()\n")
        self.write("consumer/answer.py", "ANSWER = 42\n")
        graph = self.graph()
        generated = self.node(graph, "schema/answer.pb.cc")
        self.assertIn(f"$(S)/{PROJECT}/schema/answer.proto", generated["inputs"])
        compiled = self.node(graph, "schema/answer.pb.cc.o")
        self.assertIn(generated["uid"], compiled["deps"])
        self.assertIn(self.node(graph, "schema/libschema.a")["uid"], graph["result"])
        lint = self.node(self.graph("-t"), "consumer/test-results/flake8/meta.json")
        args = [arg.replace("$(SOURCE_ROOT)", "$(S)") for command in lint["cmds"] for arg in command["cmd_args"]]
        self.assertIn(f"$(S)/{PROJECT}/consumer/answer.py", args)
