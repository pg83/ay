from tst.make.lib import MakeTest, PROJECT


class LlvmCompilationOptionsTest(MakeTest):
    def test_compile_options_reach_the_bitcode_compiler(self):
        self.write("ya.make", "LIBRARY()\nUSE_LLVM_BC16()\nSET(LLVM_OPTS -DE2E_FIRST -DE2E_SECOND)\nLLVM_COMPILE_CXX(answer.cpp answer.bc -DE2E_LAST)\nRESOURCE(answer.bc /bitcode)\nEND()\n")
        self.write("answer.cpp", 'extern "C" int answer() { return 42; }\n')
        generated = self.node(self.graph(), "answer.bc")
        args = [arg for command in generated["cmds"] for arg in command["cmd_args"]]
        for flag in ("-DE2E_FIRST", "-DE2E_SECOND", "-DE2E_LAST"):
            self.assertIn(flag, args)

    def test_source_rooted_toolchain_paths_are_preserved(self):
        self.write("ya.make", f"LIBRARY()\nUSE_LLVM_BC16()\nSET(CLANG_BC_ROOT ${{ARCADIA_ROOT}}/{PROJECT}/toolchain)\nLLVM_COMPILE_CXX(answer.cpp answer.bc)\nRESOURCE(answer.bc /bitcode)\nEND()\n")
        self.write("answer.cpp", 'extern "C" int answer() { return 42; }\n')
        generated = self.node(self.graph(), "answer.bc")
        args = [arg.replace("$(SOURCE_ROOT)", "$(S)") for command in generated["cmds"] for arg in command["cmd_args"]]
        self.assertIn(f"$(S)/{PROJECT}/toolchain/bin/clang++", args)

    def test_rooted_generated_bitcode_sources_keep_their_producer(self):
        self.write("ya.make", "LIBRARY()\nUSE_LLVM_BC16()\nCOPY_FILE(template.cpp generated.cpp)\nLLVM_BC(${BINDIR}/generated.cpp NAME answer)\nEND()\n")
        self.write("template.cpp", 'extern "C" int answer() { return 42; }\n')
        graph = self.graph()
        generated = self.node(graph, "generated.cpp")
        compiled = [node for node in graph["graph"] if generated["uid"] in node["deps"] and any(path.endswith(".bc") for path in node["outputs"])]
        self.assertTrue(compiled)
        self.assertTrue(any(f"$(B)/{PROJECT}/generated.cpp" in node["inputs"] for node in compiled))

    def test_configured_cxx_flags_reach_bitcode_compilation(self):
        config = self.root / "ya.conf"
        self.write_tree("ya.conf", config.read_text() + '\n[flags]\nCXXFLAGS = "-DE2E_CXX=42"\n')
        self.write("ya.make", "LIBRARY()\nUSE_LLVM_BC16()\nLLVM_BC(answer.cpp NAME answer)\nEND()\n")
        self.write("answer.cpp", 'extern "C" int answer() { return 42; }\n')
        generated = self.node(self.graph(), "answer.cpp.bc")
        args = [arg for command in generated["cmds"] for arg in command["cmd_args"]]
        self.assertIn("-DE2E_CXX=42", args)
