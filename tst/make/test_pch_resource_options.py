from tst.make.lib import MakeTest, PROJECT


class PchResourceOptionsTest(MakeTest):
    def fixture(self, setting=""):
        self.write("ya.make", f"LIBRARY()\nADDINCL(${{ARCADIA_BUILD_ROOT}}/{PROJECT})\n{setting}\nGEN_PCH_FILE(${{ARCADIA_ROOT}}/{PROJECT}/answer.h ${{BINDIR}}/answer.h.pch)\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.h", "#pragma once\nint answer();\n")
        self.write("answer.cpp", '#if 0\n#include "answer.h.pch"\n#endif\nint answer() { return 42; }\n')

    def test_module_llvm_root_reaches_the_precompiled_header_command(self):
        self.fixture("SET(LLVM_TOOLS_ROOT /ay_e2e_llvm)")
        args = [arg for command in self.node(self.graph(), "answer.h.pch")["cmds"] for arg in command["cmd_args"]]
        self.assertIn("/ay_e2e_llvm", args)

    def test_platform_llvm_flag_preserves_the_declared_tool_resource(self):
        self.fixture()
        args = [arg for command in self.node(self.graph("-DLLVM_TOOLS_ROOT=/ay_e2e_llvm"), "answer.h.pch")["cmds"] for arg in command["cmd_args"]]
        self.assertNotIn("/ay_e2e_llvm", args)
        self.assertTrue(any(arg.startswith("$(CLANG") for arg in args), args)

    def test_undefined_llvm_resource_globals_do_not_prevent_pch_generation(self):
        self.fixture("SET(LLVM_TOOLS_ROOT $E2E_MISSING_RESOURCE_GLOBAL)")
        self.node(self.graph(), "answer.h.pch")
