from tst.make.lib import MakeTest, PROJECT


class MultimodulesTest(MakeTest):
    def test_yql_udf(self):
        self.write("ya.make", "YQL_UDF(answer)\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", 'extern "C" int answer() { return 42; }\n')
        graph = self.graph()
        self.assertTrue(any(path.endswith(".so") for node in graph["graph"] if node["uid"] in graph["result"] for path in node["outputs"]))

    def test_sandbox_task(self):
        self.write("ya.make", "SANDBOX_PY3_TASK(answer)\nPY_SRCS(__init__.py)\nEND()\n")
        self.write("__init__.py", "from sandbox import sdk2\nclass Answer(sdk2.Task):\n    def on_execute(self):\n        pass\n")
        graph = self.graph()
        self.assertTrue(any(f"$(S)/{PROJECT}/__init__.py" in node["inputs"] for node in graph["graph"]))

    def test_mlaunch_library(self):
        self.write("ya.make", "MLAUNCH_LIBRARY(answer)\nSRCS(answer.cpp)\nGOLAUNCH_SRCS(answer.go)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        self.write("answer.go", "package answer\nfunc Answer() int { return 42 }\n")
        graph = self.graph()
        self.assertTrue(any(f"$(S)/{PROJECT}/answer.cpp" in node["inputs"] for node in graph["graph"]))

    def test_golaunch_program(self):
        self.write("ya.make", "GOLAUNCH_PROGRAM(answer)\nEND()\n")
        graph = self.graph()
        self.node(graph, "launch.go")
        self.node(graph, "answer")

    def test_python_native_module(self):
        self.write("ya.make", "PY3MODULE(answer)\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", '#include <Python.h>\nstatic PyModuleDef module = {PyModuleDef_HEAD_INIT, "answer", nullptr, -1, nullptr};\nPyMODINIT_FUNC PyInit_answer() { return PyModule_Create(&module); }\n')
        graph = self.graph()
        self.assertTrue(any(path.endswith(".so") for node in graph["graph"] if node["uid"] in graph["result"] for path in node["outputs"]))
