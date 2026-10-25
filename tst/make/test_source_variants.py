from tst.make.lib import MakeTest, PROJECT


class SourceVariantsTest(MakeTest):
    def test_cpp_comments_and_raw_string(self):
        self.write("ya.make", "LIBRARY()\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", '/* #include "absent.h" */\n// #include "also_absent.h"\n#include "value.h"\nconst char* text = R"tag(#include "ignored.h")tag";\nint answer() { return VALUE; }\n')
        self.write("value.h", "#pragma once\n#define VALUE 42\n")
        self.assertIn(f"$(S)/{PROJECT}/value.h", self.node(self.graph(), "answer.cpp.o")["inputs"])

    def test_cython_cimport(self):
        self.write("ya.make", "PY3_LIBRARY()\nPY_SRCS(answer.pyx)\nEND()\n")
        self.write("answer.pyx", 'from value cimport answer_value\ninclude "part.pxi"\ndef answer():\n    return answer_value()\n')
        self.write("value.pxd", 'cdef inline int answer_value():\n    return 42\n')
        self.write("part.pxi", 'cdef int other = 17\n')
        graph = self.graph()
        producers = [node for node in graph["graph"] if f"$(S)/{PROJECT}/answer.pyx" in node["inputs"]]
        self.assertTrue(any(f"$(S)/{PROJECT}/value.pxd" in node["inputs"] and f"$(S)/{PROJECT}/part.pxi" in node["inputs"] for node in producers))

    def test_python_two_and_three_variants(self):
        self.write("ya.make", "PY23_LIBRARY()\nPY_SRCS(answer.py)\nEND()\n")
        self.write("answer.py", "ANSWER = 42\n")
        graph = self.graph()
        self.assertTrue(any(f"$(S)/{PROJECT}/answer.py" in node["inputs"] for node in graph["graph"]))

    def test_go_embed_directory(self):
        self.write("ya.make", "GO_LIBRARY()\nALL_GO_SRCS()\nGO_EMBED_DIR(data)\nEND()\n")
        self.write("answer.go", 'package answer\nimport "embed"\n//go:embed data/*\nvar Files embed.FS\n')
        self.write("data/answer.txt", "42\n")
        graph = self.graph()
        self.assertTrue(any(f"$(S)/{PROJECT}/data/answer.txt" in node["inputs"] for node in graph["graph"]))

    def test_swig_java(self):
        self.write("ya.make", "LIBRARY()\nSRCS(answer.swg)\nEND()\n")
        self.write("answer.swg", '%module answer\n%inline %{ int answer() { return 42; } %}\n')
        self.node(self.graph(), "answer_wrap.swg.cpp")
