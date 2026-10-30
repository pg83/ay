from tst.make.lib import MakeTest, PROJECT


class RootedHeaderDirectoriesTest(MakeTest):
    def test_headers_glob_accepts_source_roots_and_skips_build_directories(self):
        self.write("ya.make", f"LIBRARY()\nSRCS(answer.cpp)\nHEADERS(${{ARCADIA_ROOT}}/{PROJECT}/include ${{BINDIR}}/generated)\nCOPY_FILE(template.h generated/ignored.h)\nSTYLE_CPP()\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        self.write("include/answer.h", "#pragma once\n")
        self.write("template.h", "#pragma once\n")
        graph = self.graph("-t")
        inputs = {path for node in graph["graph"] for path in node["inputs"]}
        self.assertIn(f"$(S)/{PROJECT}/include/answer.h", inputs)
        self.assertNotIn(f"$(B)/{PROJECT}/generated/ignored.h", inputs)
