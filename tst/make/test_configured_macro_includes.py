from tst.make.lib import MakeTest, PROJECT


class ConfiguredMacroIncludesTest(MakeTest):
    def test_configured_header_can_keep_an_unresolved_macro_include(self):
        self.write("ya.make", "LIBRARY(answer)\nSET(DONT_RESOLVE_INCLUDES yes)\nCONFIGURE_FILE(template.h.in generated.h)\nRESOURCE(generated.h /configuration)\nSRCS(answer.cpp)\nEND()\n")
        self.write("template.h.in", "#pragma once\n#include E2E_OPTIONAL_HEADER\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        graph = self.graph()
        configured = self.node(graph, "generated.h")
        self.assertIn(f"$(S)/{PROJECT}/template.h.in", configured["inputs"])
        consumers = [node for node in graph["graph"] if f"$(B)/{PROJECT}/generated.h" in node["inputs"]]
        self.assertTrue(any(configured["uid"] in node["deps"] for node in consumers))
