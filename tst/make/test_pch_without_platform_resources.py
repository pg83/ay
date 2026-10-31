from tst.make.lib import MakeTest


class PchWithoutPlatformResourcesTest(MakeTest):
    def test_precompiled_headers_keep_their_inputs_without_platform_peers(self):
        project = self.project_name
        self.write("ya.make", f"LIBRARY()\nNO_RUNTIME()\nNO_UTIL()\nSET(NEED_PLATFORM_PEERDIRS no)\nDISABLE(COMMON_LINK_SETTINGS)\nADDINCL(${{ARCADIA_BUILD_ROOT}}/{project})\nGEN_PCH_FILE(${{ARCADIA_ROOT}}/{project}/answer.h ${{BINDIR}}/answer.h.pch)\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.h", "#pragma once\nint answer();\n")
        self.write("answer.cpp", '#if 0\n#include "answer.h.pch"\n#endif\nint answer() { return 42; }\n')
        graph = self.graph()
        generated = self.node(graph, "answer.h.pch")
        self.assertIn(f"$(S)/{project}/answer.h", generated["inputs"])
        self.assertIn(generated["uid"], self.node(graph, "answer.cpp.o")["deps"])
