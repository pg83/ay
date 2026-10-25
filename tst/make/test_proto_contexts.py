from tst.make.lib import MakeTest, PROJECT


class ProtoContextsTest(MakeTest):
    def test_common_google_apis(self):
        self.write("answer.proto", f'syntax = "proto3"; package e2e; option go_package = "a.yandex-team.ru/{PROJECT}"; message Answer {{ int32 value = 1; }}\n')
        for arguments in ("", "rpc/status"):
            with self.subTest(arguments=arguments):
                self.write("ya.make", f"PROTO_LIBRARY()\nONLY_TAGS(GO_PROTO)\nUSE_COMMON_GOOGLE_APIS({arguments})\nSRCS(answer.proto)\nEND()\n")
                self.node(self.graph(), "answer.pb.go")

    def test_common_google_apis_java(self):
        self.write("ya.make", f"JAVA_PROGRAM(answer)\nJDK_VERSION(17)\nJAVA_SRCS(Main.java)\nPEERDIR({PROJECT}/schema)\nEND()\n")
        self.write("Main.java", "public class Main {}\n")
        self.write("schema/ya.make", "PROTO_LIBRARY()\nUSE_COMMON_GOOGLE_APIS()\nSRCS(answer.proto)\nEND()\n")
        self.write("schema/answer.proto", 'syntax = "proto3"; package e2e; message Answer { int32 value = 1; }\n')
        self.node(self.graph(), "answer.jar")

    def test_python_schema_variants(self):
        self.write("main.py", "print(42)\n")
        self.write("schema/ya.make", "PROTO_SCHEMA()\nSRCS(answer.proto)\nEND()\n")
        self.write("schema/answer.proto", 'syntax = "proto3"; package e2e; message Answer { int32 value = 1; }\n')
        for kind in ("PY3_PROGRAM", "PY2_PROGRAM"):
            with self.subTest(kind=kind):
                self.write("ya.make", f"{kind}(answer)\nPY_SRCS(MAIN main.py)\nPEERDIR({PROJECT}/schema)\nEND()\n")
                self.node(self.graph(), "answer")

    def test_explicit_proto_flags(self):
        self.write("ya.make", f"PROTO_LIBRARY()\nONLY_TAGS(GO_PROTO)\nSET_APPEND(GO_PROTO_OPTS module=a.yandex-team.ru/{PROJECT})\nSET(PROTO_NAMESPACE {PROJECT})\nSRCS(answer.proto)\nEND()\n")
        self.write("answer.proto", f'syntax = "proto3"; package e2e; option go_package = "a.yandex-team.ru/{PROJECT}"; message Answer {{ int32 value = 1; }}\n')
        self.graph()

    def test_unused_alice_function_specs(self):
        self.write("ya.make", "LIBRARY()\nALICE_GENERATE_FUNCTION_SPECS(DONT_ADD_TO_RESOURCE)\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        self.node(self.graph(), "answer.cpp.o")
