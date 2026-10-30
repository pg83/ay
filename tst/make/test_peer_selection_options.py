from tst.make.lib import MakeTest, PROJECT


class PeerSelectionOptionsTest(MakeTest):
    def sandbox_task(self):
        self.write("task/ya.make", "SANDBOX_PY23_3_TASK(answer)\nPY_SRCS(__init__.py)\nEND()\n")
        self.write("task/__init__.py", "from sandbox import sdk2\nclass Answer(sdk2.Task):\n    def on_execute(self):\n        pass\n")

    def test_python_consumers_select_compatible_sandbox_task_variants(self):
        self.sandbox_task()
        self.write("answer.py", "print(42)\n")
        for declaration in ("PY2_PROGRAM", "PY3_PROGRAM"):
            with self.subTest(declaration=declaration):
                self.write("ya.make", f"{declaration}(answer)\nPY_SRCS(MAIN answer.py)\nPEERDIR({PROJECT}/task)\nEND()\n")
                graph = self.graph()
                self.assertTrue(any(f"$(S)/{PROJECT}/task/__init__.py" in node["inputs"] for node in graph["graph"]))

    def test_explicit_tags_can_reject_all_sandbox_task_variants(self):
        self.sandbox_task()
        self.write("ya.make", f"LIBRARY()\nSET(PEERDIR_TAGS __EMPTY__ RESOURCE_LIB DLL_LIB)\nPEERDIR({PROJECT}/task)\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        self.assertNotEqual(self.make().returncode, 0)

    def test_native_python_peers_require_compatible_header_tags(self):
        self.write("ya.make", f"LIBRARY()\nSET(PEERDIR_TAGS __EMPTY__ RESOURCE_LIB DLL_LIB)\nPEERDIR({PROJECT}/native)\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        self.write("native/ya.make", "PY23_NATIVE_LIBRARY()\nSRCS(value.cpp)\nEND()\n")
        self.write("native/value.cpp", "int value() { return 42; }\n")
        self.assertNotEqual(self.make().returncode, 0)

    def test_explicit_proto_tags_require_an_enabled_variant(self):
        self.write("ya.make", f"LIBRARY()\nSET(PEERDIR_TAGS CPP_PROTO __EMPTY__ RESOURCE_LIB DLL_LIB)\nPEERDIR({PROJECT}/schema)\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        self.write("schema/ya.make", "PROTO_LIBRARY()\nONLY_TAGS(PY3_PROTO)\nSRCS(answer.proto)\nEND()\n")
        self.write("schema/answer.proto", 'syntax = "proto3"; message Answer { int32 value = 1; }\n')
        self.assertNotEqual(self.make().returncode, 0)

    def test_package_peers_keep_python_program_binary_variants(self):
        self.write("ya.make", f"PACKAGE(bundle)\nPEERDIR({PROJECT}/tool)\nEND()\n")
        self.write("tool/ya.make", "PY3_PROGRAM(answer)\nPY_SRCS(MAIN main.py)\nEND()\n")
        self.write("tool/main.py", "print(42)\n")
        graph = self.graph()
        binary = self.node(graph, "tool/answer")
        self.assertTrue(any(binary["uid"] in node["deps"] for node in graph["graph"] if node["uid"] in graph["result"]))
