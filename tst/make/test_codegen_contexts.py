from tst.make.lib import MakeTest, PROJECT


class CodegenContextsTest(MakeTest):
    def test_luajit_archives(self):
        self.write("ya.make", "LIBRARY()\nLJ_21_ARCHIVE(answer.lua second.lua)\nRESOURCE(LuaScripts.inc /bytecode LuaSources.inc /sources)\nEND()\n")
        self.write("answer.lua", "return 42\n")
        self.write("second.lua", "return 17\n")
        graph = self.graph()
        self.node(graph, "answer.raw")
        self.node(graph, "LuaScripts.inc")
        self.node(graph, "LuaSources.inc")

    def test_llvm_compile(self):
        self.write("answer.cpp", 'extern "C" int answer() { return 42; }\n')
        for version in (18, 20):
            with self.subTest(version=version):
                self.write("ya.make", f"LIBRARY()\nUSE_LLVM_BC{version}()\nLLVM_COMPILE_CXX(answer.cpp answer.bc -O2)\nRESOURCE(answer.bc /bitcode)\nEND()\n")
                self.node(self.graph(), "answer.bc")

    def test_nasm_and_preinclude(self):
        self.write("ya.make", "LIBRARY()\nUSE_NASM()\nASM_PREINCLUDE(defs.inc)\nSRCS(answer.asm)\nEND()\n")
        self.write("defs.inc", "%define VALUE 42\n")
        self.write("answer.asm", "section .text\nglobal answer\nanswer:\nmov eax, VALUE\nret\n")
        self.node(self.graph(), "answer.o")

    def test_gnu_assembly(self):
        self.write("ya.make", "LIBRARY()\nSRCS(answer.S)\nEND()\n")
        self.write("answer.S", ".text\n.globl answer\nanswer:\nmov $42, %eax\nret\n")
        self.node(self.graph(), "answer.S.o")

    def test_linker_script(self):
        self.write("ya.make", "PROGRAM(answer)\nSRCS(main.cpp)\nLINKER_SCRIPT(link.ld)\nEND()\n")
        self.write("main.cpp", "int main() { return 0; }\n")
        self.write("link.ld", "SECTIONS { .e2e : { *(.e2e) } } INSERT AFTER .text;\n")
        self.assertIn(f"$(S)/{PROJECT}/link.ld", self.node(self.graph(), "answer")["inputs"])

    def test_cython_build_flags(self):
        self.write("answer.pyx", "def answer():\n    return 42\n")
        for macro in ("BUILDWITH_CYTHON_C", "BUILDWITH_CYTHON_CPP"):
            with self.subTest(macro=macro):
                self.write("ya.make", f"PY3_LIBRARY()\n{macro}(answer.pyx)\nEND()\n")
                graph = self.graph()
                self.assertTrue(any(path.endswith((".c", ".cpp")) for node in graph["graph"] for path in node["outputs"]))

    def test_yaff(self):
        self.write("answer.proto", 'syntax = "proto2";\npackage e2e;\nmessage Answer { optional int32 value = 1; }\n')
        for macro, suffix in (("YAFF(NAMESPACE NE2E FILES answer.proto EXPERIMENTAL answer.proto)", "answer.yaff.h"), ("YAFF_SCHEMA(example NE2E FILES answer.proto EXPERIMENTAL answer.proto)", "answer_example.yaff.h")):
            with self.subTest(macro=macro):
                self.write("ya.make", f"PROTO_LIBRARY()\n{macro}\nSRCS(answer.proto)\nEND()\n")
                self.node(self.graph(), suffix)

    def test_golaunch_component(self):
        self.write("answer.cpp", "int answer() { return 42; }\n")
        for selectors, go in (
            ("ONLY_TAGS(GO_MLAUNCH)", True),
            ("ONLY_TAGS(CPP_MLAUNCH)\nINCLUDE_TAGS(GO_MLAUNCH)\nEXCLUDE_TAGS(CPP_MLAUNCH)", True),
            ("ONLY_TAGS(CPP_MLAUNCH GO_MLAUNCH)\nEXCLUDE_TAGS(GO_MLAUNCH)\nINCLUDE_TAGS(GO_MLAUNCH)\nEXCLUDE_TAGS(CPP_MLAUNCH)", True),
            ("ONLY_TAGS(CPP_MLAUNCH GO_MLAUNCH)\nEXCLUDE_TAGS(GO_MLAUNCH)", False),
        ):
            with self.subTest(selectors=selectors):
                self.write("ya.make", f"MLAUNCH_COMPONENT(answer)\n{selectors}\nIF(MODULE_TAG == CPP_MLAUNCH)\nSRCS(answer.cpp)\nENDIF()\nNO_GOLAUNCH_SRCS()\nGOLAUNCH_OVERRIDE(e2e Launch)\nGENERATE_GOLAUNCH_CALLBACKS(e2e)\nEND()\n")
                graph = self.graph()
                archive = self.node(graph, "answer.a" if go else "libanswer.a")
                self.assertEqual(graph["result"], [archive["uid"]])
                if go:
                    launch = self.node(graph, "launch.go")
                    self.assertIn(launch["uid"], archive["deps"])
                else:
                    self.assertFalse(any(f"$(B)/{PROJECT}/launch.go" in node["outputs"] for node in graph["graph"]))

    def test_golaunch_composite(self):
        self.write("ya.make", f"GOLAUNCH_COMPOSITE(answer)\nGOLAUNCH_COMPOSITE_COMPONENTS({PROJECT}/component)\nEND()\n")
        self.write("component/ya.make", "MLAUNCH_COMPONENT(component)\nONLY_TAGS(GO_MLAUNCH)\nNO_GOLAUNCH_SRCS()\nEND()\n")
        graph = self.graph()
        self.node(graph, "main.go")
        self.node(graph, "answer")

    def test_bundle_output(self):
        self.write("ya.make", f"LIBRARY()\nBUNDLE_OUTPUT({PROJECT}/tool answer.debug RENAME renamed.debug)\nRESOURCE(renamed.debug /debug)\nEND()\n")
        self.write("tool/ya.make", "PROGRAM(answer)\nSRCS(main.cpp)\nEND()\n")
        self.write("tool/main.cpp", "int main() { return 0; }\n")
        self.node(self.graph(), "renamed.debug")
