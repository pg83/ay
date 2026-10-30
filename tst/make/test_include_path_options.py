from tst.make.lib import MakeTest, PROJECT


def command_args(node):
    return [arg.replace("$(SOURCE_ROOT)", "$(S)").replace("$(BUILD_ROOT)", "$(B)") for command in node["cmds"] for arg in command["cmd_args"]]


class IncludePathOptionsTest(MakeTest):
    def test_missing_include_directories_are_configuration_errors(self):
        self.write("ya.make", f"LIBRARY()\nADDINCL({PROJECT}/missing {PROJECT}/include GLOBAL {PROJECT}/missing_global {PROJECT}/exported)\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", '#include "answer.h"\nint answer() { return VALUE; }\n')
        self.write("include/answer.h", "#define VALUE 42\n")
        self.write("exported/.keep", "")
        result = self.make()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn(f"{PROJECT}/missing", result.stderr)

    def test_self_include_directory_for_cython(self):
        self.write("ya.make", "PY3_LIBRARY()\nADDINCLSELF(FOR cython)\nPY_SRCS(answer.pyx)\nEND()\n")
        self.write("answer.pyx", "from dependency cimport value\ndef answer():\n    return value\n")
        self.write("dependency.pxd", "cdef int value\n")
        generated = self.node(self.graph(), "answer.pyx.cpp")
        self.assertIn(f"-I$(S)/{PROJECT}", command_args(generated))
        self.assertIn(f"$(S)/{PROJECT}/dependency.pxd", generated["inputs"])

    def test_self_include_directory_for_assembly(self):
        self.write("ya.make", "LIBRARY()\nADDINCLSELF(FOR asm)\nSRCS(answer.asm)\nEND()\n")
        self.write("answer.asm", '%include "value.inc"\nsection .text\nglobal answer\nanswer:\nmov eax, VALUE\nret\n')
        self.write("value.inc", "%define VALUE 42\n")
        compiled = self.node(self.graph(), "answer.o")
        args = command_args(compiled)
        self.assertTrue(any(arg == "-I" and args[index + 1] == f"$(S)/{PROJECT}" for index, arg in enumerate(args[:-1])))
        self.assertIn(f"$(S)/{PROJECT}/value.inc", compiled["inputs"])

    def test_generated_proto_imports_use_build_include_directories(self):
        for macro in (f"PROTO_NAMESPACE(WITH_GEN {PROJECT})", f"PROTO_ADDINCL(GLOBAL {PROJECT} WITH_GEN)"):
            with self.subTest(macro=macro):
                self.write("ya.make", f"PROTO_LIBRARY()\nONLY_TAGS(CPP_PROTO)\n{macro}\nCOPY_FILE(template.proto generated.proto)\nSRCS(answer.proto generated.proto)\nEND()\n")
                self.write("answer.proto", 'syntax = "proto3";\nimport "generated.proto";\nmessage Answer { Generated value = 1; }\n')
                self.write("template.proto", 'syntax = "proto3";\nmessage Generated { int32 value = 1; }\n')
                graph = self.graph()
                generated = self.node(graph, "generated.proto")
                protoc = self.node(graph, "answer.pb.cc")
                self.assertIn(f"$(B)/{PROJECT}/generated.proto", protoc["inputs"])
                self.assertIn(generated["uid"], protoc["deps"])
                self.assertIn(f"-I=$(B)/{PROJECT}", command_args(protoc))
