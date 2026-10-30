from tst.make.lib import MakeTest, PROJECT


class PythonProtoResourceNamesTest(MakeTest):
    def proto(self):
        self.write("answer.proto", 'syntax = "proto2"; package e2e; message Answer {}\n')

    def test_python_two_proto_resources_use_dotted_module_names(self):
        self.write("ya.make", f"PY2_PROGRAM(answer)\nPY_SRCS(MAIN main.py)\nPEERDIR({PROJECT}/schema)\nEND()\n")
        self.write("main.py", "print(42)\n")
        self.write("schema/ya.make", "PROTO_LIBRARY()\nONLY_TAGS(PY_PROTO)\nSRCS(answer.proto)\nEND()\n")
        self.write("schema/answer.proto", 'syntax = "proto2"; package e2e; message Answer {}\n')
        graph = self.graph()
        self.node(graph, "schema/answer__intpy2___pb2.py")
        args = [arg for node in graph["graph"] for command in node["cmds"] for arg in command["cmd_args"]]
        self.assertIn(f"resfs/src//py_modules/{PROJECT}.schema.answer_pb2={PROJECT}/schema/answer__intpy2___pb2.py", args)

    def test_explicit_proto_module_name_reaches_its_resource(self):
        self.proto()
        self.write("ya.make", "PY3_LIBRARY()\nPY_SRCS(answer.proto=custom.answer)\nEND()\n")
        args = [arg for node in self.graph()["graph"] for command in node["cmds"] for arg in command["cmd_args"]]
        self.assertIn("-resfs/file/py/custom/answer_pb2.py", args)

    def test_two_proto_module_names_for_the_same_source_are_rejected(self):
        self.proto()
        self.write("ya.make", "PY3_LIBRARY()\nPY_SRCS(answer.proto=first.answer)\nPY_SRCS(answer.proto=second.answer)\nEND()\n")
        self.assertNotEqual(self.make().returncode, 0)
