from tst.make.lib import MakeTest, PROJECT


class PchExportedHeadersTest(MakeTest):
    def test_exported_header_keeps_the_library_precompiled_header_context(self):
        self.write("ya.make", f"LIBRARY()\nPEERDIR({PROJECT}/lib)\nADDINCL({PROJECT}/consumer_include)\nCFLAGS(-DE2E_CONSUMER)\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", f'#include <{PROJECT}/lib/public.h>\nint answer() {{ return 42; }}\n')
        self.write("consumer_include/value.h", "#pragma once\n")
        self.write("lib/ya.make", f"LIBRARY()\nADDINCL(GLOBAL ${{ARCADIA_BUILD_ROOT}}/{PROJECT})\nCFLAGS(-DE2E_LIBRARY)\nGEN_PCH_FILE(${{ARCADIA_ROOT}}/{PROJECT}/lib/value.h ${{BINDIR}}/value.h.pch)\nSRCS(native.cpp public.h)\nEND()\n")
        self.write("lib/value.h", "#pragma once\n")
        self.write("lib/public.h", '#pragma once\n#if 0\n#include "lib/value.h.pch"\n#endif\n')
        self.write("lib/native.cpp", "int native() { return 42; }\n")
        graph = self.graph()
        pch = self.node(graph, "lib/value.h.pch")
        args = [arg for command in pch["cmds"] for arg in command["cmd_args"]]
        self.assertIn("-DE2E_LIBRARY", args)
        self.assertNotIn("-DE2E_CONSUMER", args)
        self.assertIn(pch["uid"], self.node(graph, "answer.cpp.o")["deps"])
