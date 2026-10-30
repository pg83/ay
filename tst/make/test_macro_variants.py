from tst.make.lib import MakeTest, PROJECT


class MacroVariantsTest(MakeTest):
    def test_no_debug_info(self):
        self.write("ya.make", "LIBRARY()\nNO_DEBUG_INFO()\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        node = self.node(self.graph(), "answer.cpp.o")
        args = [arg for command in node["cmds"] for arg in command["cmd_args"]]
        self.assertNotIn("-g", args)
        self.assertNotIn("-gline-tables-only", args)

    def test_native_options(self):
        variants = (
            "NO_COMPILER_WARNINGS()\nNO_WSHADOW()\nNO_OPTIMIZE()\nSTRIP()",
            "SPLIT_DWARF()\nCFLAGS(-DE2E=42)\nLDFLAGS(-Wl,--as-needed)",
            "NO_SPLIT_DWARF()\nNO_EXPORT_DYNAMIC_SYMBOLS()\nNO_LINT()",
            "VERSION(1 2 3)\nLICENSE(MIT)\nALLOCATOR(SYSTEM)",
            "COMPILE_C_AS_CXX()\nNO_SANITIZE()",
        )
        self.write("main.cpp", "int main() { return 0; }\n")
        for macros in variants:
            with self.subTest(macros=macros):
                self.write("ya.make", f"PROGRAM(answer)\n{macros}\nSRCS(main.cpp)\nEND()\n")
                self.node(self.graph(), "answer")

    def test_simd_sources(self):
        self.write("ya.make", "LIBRARY()\nSRC_C_SSE2(answer.cpp)\nSRC_C_AVX2(other.cpp)\nSRC_C_NO_LTO(third.cpp)\nEND()\n")
        for name in ("answer", "other", "third"):
            self.write(name + ".cpp", f"int {name}() {{ return 42; }}\n")
        graph = self.graph()
        for name in ("answer", "other", "third"):
            self.assertTrue(any(f"$(S)/{PROJECT}/{name}.cpp" in node["inputs"] and any(path.endswith(".o") for path in node["outputs"]) for node in graph["graph"]))

    def test_cuda_device_link(self):
        self.write("ya.make", "LIBRARY()\nCUDA_SRCS(answer.cu)\nCUDA_NVCC_FLAGS(-lineinfo)\nNVCC_DEVICE_LINK()\nEND()\n")
        self.write("answer.cu", "__global__ void answer(int* value) { *value = 42; }\n")
        graph = self.graph()
        self.assertTrue(any(f"$(S)/{PROJECT}/answer.cu" in node["inputs"] for node in graph["graph"]))

    def test_bison_c(self):
        self.write("ya.make", "LIBRARY()\nBISON_GEN_C()\nBISON_FLAGS(--warnings=none)\nSRCS(parser.y)\nEND()\n")
        self.write("parser.y", "%token NUMBER\n%%\nexpression: NUMBER;\n%%\n")
        self.node(self.graph(), "parser.y.c")

    def test_bison_cpp(self):
        self.write("ya.make", "LIBRARY()\nBISON_GEN_CPP()\nBISON_FLAGS(--warnings=none)\nSRCS(parser.y)\nEND()\n")
        self.write("parser.y", "%token NUMBER\n%%\nexpression: NUMBER;\n%%\n")
        node = self.node(self.graph(), "parser.y.cpp")
        self.assertIn(f"$(S)/{PROJECT}/parser.y", node["inputs"])

    def test_build_info(self):
        self.write("ya.make", "LIBRARY()\nCREATE_BUILDINFO_FOR(buildinfo.h)\nRESOURCE(buildinfo.h /buildinfo)\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", 'int answer() { return 42; }\n')
        self.node(self.graph(), "buildinfo.h")

    def test_llvm_machine_code(self):
        self.write("ya.make", "LIBRARY()\nUSE_LLVM_BC16()\nLLVM_BC(answer.cpp NAME answer SUFFIX .e2e GENERATE_MACHINE_CODE SYMBOLS answer)\nEND()\n")
        self.write("answer.cpp", 'extern "C" int answer() { return 42; }\n')
        graph = self.graph()
        optimized = self.node(graph, "answer_optimized.e2e.bc")
        obj = self.node(graph, "answer_optimized.e2e.bc.o")
        self.assertIn(optimized["uid"], obj["deps"])
        archive = self.node(graph, "lib__ay_e2e__.a")
        self.assertIn(obj["uid"], archive["deps"])
