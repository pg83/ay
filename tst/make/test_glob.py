from tst.make.lib import MakeTest, PROJECT


class GlobTest(MakeTest):
    def test_headers_suffix_alternatives(self):
        selected = tuple(f"include/answer.{ext}" for ext in ("h", "H", "hh", "hpp", "hxx", "ipp"))
        for name in (*selected, "include/skip.h", "include/answer.txt", "include/sub/nested.h"):
            self.write(name, "#pragma once\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        self.write("ya.make", 'LIBRARY()\nSRCS(answer.cpp)\nHEADERS(include EXCLUDE include/skip.h)\nSTYLE_CPP()\nEND()\n')
        inputs = {path for node in self.graph("-t")["graph"] for path in node.get("inputs", [])}
        for name in selected:
            self.assertIn(f"$(S)/{PROJECT}/{name}", inputs)
        for name in ("include/skip.h", "include/answer.txt", "include/sub/nested.h"):
            self.assertNotIn(f"$(S)/{PROJECT}/{name}", inputs)
