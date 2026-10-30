from tst.make.lib import MakeTest


class PremoduleProducerDeclarationsTest(MakeTest):
    def test_ragel_producers_require_a_module(self):
        self.write("ya.make", "BUILDWITH_RAGEL6(lexer.rl)\nLIBRARY()\nSRCS(answer.cpp)\nEND()\n")
        self.write("lexer.rl", "%%{ machine scanner; main := any*; }%%\n%% write data;\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        self.assertNotEqual(self.make().returncode, 0)

    def test_enum_serialization_producers_require_a_module(self):
        self.write("ya.make", "GENERATE_ENUM_SERIALIZATION(answer.h)\nLIBRARY()\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.h", "#pragma once\nenum class Answer { Value = 42 };\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        self.assertNotEqual(self.make().returncode, 0)

    def test_joined_source_producers_require_a_module(self):
        self.write("ya.make", "JOIN_SRCS(generated.cpp first.cpp second.cpp)\nLIBRARY()\nSRCS(answer.cpp)\nEND()\n")
        for name in ("first", "second", "answer"):
            self.write(name + ".cpp", f"int {name}() {{ return 42; }}\n")
        self.assertNotEqual(self.make().returncode, 0)

    def test_archive_assembly_producers_require_a_module(self):
        self.write("ya.make", "ARCHIVE_ASM(NAME data value.txt)\nLIBRARY()\nSRCS(answer.cpp)\nEND()\n")
        self.write("value.txt", "42\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        self.assertNotEqual(self.make().returncode, 0)

    def test_sandbox_producers_require_a_module(self):
        self.write("ya.make", "FROM_SANDBOX(FILE 123456789 OUT_NOAUTO value.txt)\nLIBRARY()\nRESOURCE(value.txt /value)\nEND()\n")
        self.assertNotEqual(self.make().returncode, 0)

    def test_llvm_bitcode_producers_require_a_module(self):
        self.write("ya.make", "LLVM_BC(answer.cpp NAME answer)\nLIBRARY()\nUSE_LLVM_BC16()\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        self.assertNotEqual(self.make().returncode, 0)

    def test_split_codegen_producers_require_a_module(self):
        self.write("ya.make", f"SPLIT_CODEGEN({self.project_name}/generator answer OUT_NUM 6)\nLIBRARY()\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        self.write("generator/ya.make", "PY3_PROGRAM(generator)\nPY_SRCS(MAIN main.py)\nEND()\n")
        self.write("generator/main.py", "print(42)\n")
        self.write("answer.in", "42\n")
        self.assertNotEqual(self.make().returncode, 0)

    def test_go_gateway_producers_require_a_module(self):
        self.write("ya.make", "GO_GRPC_GATEWAY_V2_OPENAPI_SRCS(answer.proto)\nGO_LIBRARY()\nSRCS(answer.go)\nEND()\n")
        self.write("answer.proto", 'syntax = "proto3"; message Answer { int32 value = 1; }\n')
        self.write("answer.go", "package answer\n")
        self.assertNotEqual(self.make().returncode, 0)

    def test_java_program_producers_require_a_module(self):
        self.write("ya.make", f"JDK_VERSION(17)\nRUN_JAVA_PROGRAM(Generator CLASSPATH {self.project_name}/generator OUT generated.cpp)\nLIBRARY()\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        self.write("generator/ya.make", "JAVA_PROGRAM(generator)\nJDK_VERSION(17)\nJAVA_SRCS(Generator.java)\nEND()\n")
        self.write("generator/Generator.java", 'public class Generator { public static void main(String[] args) {} }\n')
        self.assertNotEqual(self.make().returncode, 0)

    def test_go_oapi_producers_require_a_module(self):
        self.write("ya.make", "GO_OAPI_CODEGEN(types answer api.yaml)\nGO_LIBRARY()\nSRCS(answer.go)\nEND()\n")
        self.write("api.yaml", "openapi: 3.0.0\ninfo:\n  title: Answer\n  version: 1.0.0\npaths: {}\n")
        self.write("answer.go", "package answer\n")
        self.assertNotEqual(self.make().returncode, 0)

    def test_cuda_producers_require_a_module(self):
        self.write("ya.make", "SET(CUDA_ARCHITECTURES sm_75)\nCUDA_SRCS(answer.cu)\nLIBRARY()\nSRCS(base.cpp)\nEND()\n")
        self.write("answer.cu", "__global__ void answer(int* value) { *value = 42; }\n")
        self.write("base.cpp", "int base() { return 42; }\n")
        self.assertNotEqual(self.make().returncode, 0)
