import json

from tst.make.lib import MakeTest, PROJECT


class ModuleOutputsTest(MakeTest):
    def test_bundled_program(self):
        self.write("ya.make", f"LIBRARY()\nBUNDLE({PROJECT}/tool NAME bundled)\nRESOURCE(bundled /bundled)\nEND()\n")
        self.write("tool/ya.make", "PROGRAM(tool)\nSRCS(main.cpp)\nEND()\n")
        self.write("tool/main.cpp", "int main() { return 0; }\n")
        self.node(self.graph(), "bundled")

    def test_dll_for(self):
        self.write("ya.make", f"DLL_FOR({PROJECT}/lib answer)\n")
        self.write("lib/ya.make", "LIBRARY()\nIF (OS_LINUX)\nSRCS(answer.cpp)\nENDIF()\nEND()\n")
        self.write("lib/answer.cpp", 'extern "C" int answer() { return 42; }\n')
        graph = self.graph()
        self.assertTrue(any(path.endswith(".so") for node in graph["graph"] if node["uid"] in graph["result"] for path in node["outputs"]))

    def test_generated_library(self):
        self.write("ya.make", f"GEN_LIBRARY(answer)\nPEERDIR({PROJECT}/lib)\nRUN_PYTHON3(generate.py STDOUT_NOAUTO generated.txt)\nEND()\n")
        self.write("generate.py", "print(42)\n")
        self.write("lib/ya.make", "LIBRARY()\nSRCS(answer.cpp)\nEND()\n")
        self.write("lib/answer.cpp", "int answer() { return 42; }\n")
        self.node(self.graph(), "generated.txt")

    def test_large_local_file(self):
        self.write("ya.make", "LIBRARY()\nLARGE_FILES(AUTOUPDATED data.txt)\nRESOURCE(data.txt /data)\nEND()\n")
        self.write("data.txt", "42\n")
        graph = self.graph()
        self.assertTrue(any(f"$(S)/{PROJECT}/data.txt" in node["inputs"] for node in graph["graph"]))

    def test_large_remote_file(self):
        self.write("ya.make", "LIBRARY()\nLARGE_FILES(data.txt)\nRESOURCE(data.txt /data)\nEND()\n")
        self.write("data.txt.external", json.dumps({"storage": "SANDBOX", "resource_id": 123456789}))
        self.node(self.graph(), "data.txt")

    def test_proto_documentation(self):
        self.write("ya.make", "PROTO_LIBRARY()\nONLY_TAGS(DOCS_PROTO)\nSRCS(answer.proto)\nEND()\n")
        self.write("answer.proto", 'syntax = "proto3"; package e2e; message Answer { int32 value = 1; }\n')
        self.node(self.graph(), "answer.md")

    def test_go_flatbuffers(self):
        self.write("ya.make", f"GO_LIBRARY(answer)\nFBS_NAMESPACE(e2e a.yandex-team.ru/{PROJECT})\nSRCS(answer.fbs)\nEND()\n")
        self.write("answer.fbs", "namespace e2e; table Answer { value: int; } root_type Answer;\n")
        self.node(self.graph(), "answer.fbs.gosrc")

    def test_fortran(self):
        self.write("ya.make", "LIBRARY()\nSRCS(answer.f)\nEND()\n")
        self.write("answer.f", "      INTEGER FUNCTION ANSWER()\n      ANSWER = 42\n      END\n")
        graph = self.graph()
        self.node(graph, "answer.f.o")
