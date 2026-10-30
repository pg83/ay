from tst.make.lib import MakeTest


class BuildRootIncludeDedupTest(MakeTest):
    def test_generated_sources_at_the_build_root_keep_one_header_dependency(self):
        self.write("ya.make", "LIBRARY()\nADDINCL(${ARCADIA_ROOT} ${ARCADIA_BUILD_ROOT})\nFROM_SANDBOX(FILE 123456789 OUT_NOAUTO ${ARCADIA_BUILD_ROOT}/__ay_root_e2e__.cpp OUTPUT_INCLUDES __ay_root_e2e__.h)\nCOPY_FILE(template.h ${ARCADIA_BUILD_ROOT}/__ay_root_e2e__.h)\nSRCS(${ARCADIA_BUILD_ROOT}/__ay_root_e2e__.cpp)\nEND()\n")
        self.write("template.h", "#pragma once\n")
        graph = self.graph()
        compiled = [node for node in graph["graph"] if "$(B)/__ay_root_e2e__.cpp" in node["inputs"] and any(path.endswith(".o") for path in node["outputs"])]
        self.assertEqual(len(compiled), 1)
        self.assertEqual(compiled[0]["inputs"].count("$(B)/__ay_root_e2e__.h"), 1)
