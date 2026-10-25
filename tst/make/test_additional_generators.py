import io
import tarfile

from tst.make.lib import MakeTest, PROJECT


class AdditionalGeneratorsTest(MakeTest):
    def test_gazetteer_proto(self):
        self.write("ya.make", "LIBRARY()\nSRCS(answer.gztproto)\nEND()\n")
        self.write("answer.gztproto", 'package e2e; message Answer { optional int32 value = 1; }\n')
        self.node(self.graph(), "answer.proto")

    def test_antlr_cpp(self):
        self.write("ya.make", "LIBRARY()\nRUN_ANTLR4_CPP(Answer.g4 VISITOR LISTENER)\nEND()\n")
        self.write("Answer.g4", "grammar Answer;\nanswer: NUMBER EOF;\nNUMBER: [0-9]+;\nWS: [ \\t\\r\\n]+ -> skip;\n")
        self.node(self.graph(), "AnswerParser.cpp")

    def test_antlr_python(self):
        self.write("ya.make", "PY3_LIBRARY()\nRUN_ANTLR4_PYTHON3(Answer.g4 VISITOR LISTENER)\nPY_SRCS(AnswerParser.py AnswerLexer.py)\nEND()\n")
        self.write("Answer.g4", "grammar Answer;\nanswer: NUMBER EOF;\nNUMBER: [0-9]+;\n")
        self.node(self.graph(), "AnswerParser.py")

    def test_lua_bytecode(self):
        self.write("ya.make", "LIBRARY()\nCOMPILE_LUA_21(answer.lua NAME answer)\nEND()\n")
        self.write("answer.lua", "return 42\n")
        graph = self.graph()
        self.assertTrue(any(f"$(S)/{PROJECT}/answer.lua" in node["inputs"] for node in graph["graph"]))

    def test_bpf_object(self):
        self.write("ya.make", "LIBRARY()\nBPF_STATIC(answer.c answer.bpf.o)\nEND()\n")
        self.write("answer.c", "int answer(void) { return 42; }\n")
        self.node(self.graph(), "answer.bpf.o")

    def test_userver_generated_source(self):
        self.write("ya.make", "LIBRARY()\nUSRV_BUILD(answer.cpp FROM sources.tar DEPS_FILE deps.json)\nEND()\n")
        self.write("deps.json", '{"answer.cpp": []}\n')
        source = b"int answer() { return 42; }\n"
        with tarfile.open(self.project / "sources.tar", "w") as archive:
            entry = tarfile.TarInfo("answer.cpp")
            entry.size = len(source)
            archive.addfile(entry, io.BytesIO(source))
        graph = self.graph()
        self.node(graph, "answer.cpp")
        self.node(graph, "answer.cpp.o")

    def test_userver_grpc_client(self):
        self.write("ya.make", f"LIBRARY()\nUSRV_GEN_GRPC_CLIENT_V2({PROJECT}/proto/answer.proto FROM {PROJECT}/proto)\nEND()\n")
        self.write("proto/ya.make", "PROTO_LIBRARY()\nGRPC()\nSRCS(answer.proto)\nEND()\n")
        self.write("proto/answer.proto", 'syntax = "proto3"; package e2e; message Answer { int32 value = 1; } service Answers { rpc Get(Answer) returns (Answer); }\n')
        graph = self.graph()
        self.assertTrue(any(f"$(S)/{PROJECT}/proto/answer.proto" in node["inputs"] and any("client" in path for path in node["outputs"]) for node in graph["graph"]))
