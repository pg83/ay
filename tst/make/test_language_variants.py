from tst.make.lib import MakeTest, PROJECT


class LanguageVariantsTest(MakeTest):
    def test_python_swig(self):
        self.write("ya.make", "PY3_LIBRARY()\nSRCS(answer.swg)\nEND()\n")
        self.write("answer.swg", '%module answer\n%inline %{ int answer() { return 42; } %}\n')
        graph = self.graph()
        self.assertIn(f"$(S)/{PROJECT}/answer.swg", self.node(graph, "answer_wrap.swg.cpp")["inputs"])

    def test_llvm_explicit_toolchain(self):
        self.write("ya.make", "LIBRARY()\nUSE_LLVM_BC16()\nLLVM_BC(answer.c NAME answer SYMBOLS answer)\nEND()\n")
        self.write("answer.c", "int answer(void) { return 42; }\n")
        graph = self.graph()
        self.node(graph, "answer.c.bc")
        self.node(graph, "answer_optimized.bc")

    def test_go_program(self):
        self.write("ya.make", f"GO_PROGRAM(answer)\nSRCS(main.go)\nPEERDIR({PROJECT}/lib)\nEND()\n")
        self.write("main.go", f'package main\nimport "a.yandex-team.ru/{PROJECT}/lib"\nfunc main() {{ println(answer.Answer()) }}\n')
        self.write("lib/ya.make", "GO_LIBRARY()\nSRCS(answer.go)\nEND()\n")
        self.write("lib/answer.go", "package answer\nfunc Answer() int { return 42 }\n")
        graph = self.graph()
        self.assertIn(self.node(graph, "answer")["uid"], graph["result"])

    def test_go_cgo(self):
        self.write("ya.make", "GO_LIBRARY()\nSET(CGO_ENABLED yes)\nCGO_SRCS(answer.go)\nSRCS(answer.c)\nEND()\n")
        self.write("answer.go", 'package answer\n/*\nint answer(void);\n*/\nimport "C"\nfunc Answer() int { return int(C.answer()) }\n')
        self.write("answer.c", "int answer(void) { return 42; }\n")
        graph = self.graph()
        self.assertTrue(any(any("cgo" in output for output in node["outputs"]) and f"$(S)/{PROJECT}/answer.go" in node["inputs"] for node in graph["graph"]))

    def test_python_proto_peer(self):
        self.write("ya.make", f"PY3_PROGRAM(answer)\nPEERDIR({PROJECT}/proto)\nPY_SRCS(MAIN answer.py)\nEND()\n")
        self.write("answer.py", "ANSWER = 42\n")
        self.write("proto/ya.make", "PROTO_LIBRARY()\nSRCS(message.proto)\nEND()\n")
        self.write("proto/message.proto", 'syntax = "proto3"; package e2e; message Answer { int32 value = 1; }\n')
        graph = self.graph()
        self.node(graph, "proto/message__intpy3___pb2.py")

    def test_java_proto_peer(self):
        self.write("ya.make", f"JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nPEERDIR({PROJECT}/proto)\nJAVA_SRCS(Answer.java)\nEND()\n")
        self.write("Answer.java", "package e2e; public class Answer {}\n")
        self.write("proto/ya.make", "PROTO_LIBRARY()\nSRCS(message.proto)\nEND()\n")
        self.write("proto/message.proto", 'syntax = "proto3"; package e2e; message Answer { int32 value = 1; }\n')
        graph = self.graph()
        self.assertTrue(any(f"$(S)/{PROJECT}/proto/message.proto" in node["inputs"] and any(path.endswith((".jar", ".jsrc")) for path in node["outputs"]) for node in graph["graph"]))
