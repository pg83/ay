from tst.make.lib import MakeTest, PROJECT


class ScannerBoundariesTest(MakeTest):
    def test_fortran_single_quoted_includes_reach_compilation_inputs(self):
        self.write("ya.make", "LIBRARY()\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", '#include "answer.f"\nint answer() { return 42; }\n')
        self.write("answer.f", "      include 'value.inc'\n      INTEGER FUNCTION ANSWER()\n      ANSWER = VALUE\n      END\n")
        self.write("value.inc", "      INTEGER VALUE\n      PARAMETER (VALUE=42)\n")
        self.assertIn(f"$(S)/{PROJECT}/value.inc", self.node(self.graph(), "answer.cpp.o")["inputs"])

    def test_empty_and_unclosed_fortran_includes_do_not_create_inputs(self):
        self.write("ya.make", "LIBRARY()\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", '#include "answer.f"\nint answer() { return 42; }\n')
        self.write("answer.f", "      include ''\n      include 'unfinished\n      END\n")
        self.node(self.graph(), "answer.cpp.o")

    def test_yasm_only_scans_directives_adjacent_to_the_percent_sign(self):
        self.write("ya.make", f"LIBRARY()\nADDINCLSELF(FOR asm)\nADDINCL({PROJECT})\nSRCS(answer.asm)\nEND()\n")
        self.write("answer.asm", '% \tinclude <value.inc>\n%include "other.inc"\nsection .text\n')
        self.write("value.inc", "%define VALUE 42\n")
        self.write("other.inc", "%define OTHER 1\n")
        inputs = self.node(self.graph(), "answer.o")["inputs"]
        self.assertIn(f"$(S)/{PROJECT}/other.inc", inputs)
        self.assertNotIn(f"$(S)/{PROJECT}/value.inc", inputs)

    def test_short_and_unclosed_yasm_directives_do_not_create_inputs(self):
        self.write("ya.make", "LIBRARY()\nSRCS(answer.asm)\nEND()\n")
        self.write("answer.asm", '% \ti\n%include\n%include ""\n%include "unfinished\nsection .text\n')
        self.node(self.graph(), "answer.o")

    def swig_generator(self, text):
        self.write("ya.make", "PY3_LIBRARY()\nPY_SRCS(SWIG_CPP answer.swg)\nEND()\n")
        self.write("answer.swg", "%module answer\n" + text)
        return next(node for node in self.graph()["graph"]
                    if f"$(S)/{PROJECT}/answer.swg" in node["inputs"]
                    and any(path.endswith(".cpp") for path in node["outputs"]))

    def test_swig_imports_and_insert_sections_keep_their_dependencies(self):
        self.write("other.swg", "%module other\n")
        self.write("value.h", "int answer();\n")
        inputs = self.swig_generator('%import "other.swg"\n%insert (header) "value.h"\n')["inputs"]
        for name in ("other.swg", "value.h"):
            self.assertIn(f"$(S)/{PROJECT}/{name}", inputs)

    def test_incomplete_swig_insert_sections_do_not_create_inputs(self):
        self.swig_generator('%insert\n%insert header "missing.h"\n%insert (header\n%insert ()\n')

    def test_cfgproto_ignores_incomplete_include_options(self):
        self.write("ya.make", f"LIBRARY()\nADDINCL({PROJECT})\nSRCS(answer.cfgproto)\nEND()\n")
        self.write("answer.cfgproto", '''syntax = "proto2";
package e2e;
option (NProtoConfig.Include) = "value.h"; // dependency
option (Other.Include) = "missing.h";
option (NProtoConfig.Include) "missing.h";
option (NProtoConfig.Include) = 42;
message Answer { optional int32 value = 1; }
''')
        self.write("value.h", "#pragma once\n")
        graph = self.graph()
        generated = [node for node in graph["graph"]
                     if any(path.endswith(".cc") for path in node["outputs"])
                     and f"$(S)/{PROJECT}/answer.cfgproto" in node["inputs"]]
        self.assertTrue(generated)
        self.assertTrue(any(f"$(S)/{PROJECT}/value.h" in node["inputs"] for node in graph["graph"]))

    def test_empty_and_unclosed_flatbuffers_includes_do_not_create_inputs(self):
        self.write("ya.make", "FBS_LIBRARY()\nONLY_TAGS(CPP_FBS)\nSRCS(answer.fbs)\nEND()\n")
        self.write("answer.fbs", 'include "";\ninclude "unfinished\nnamespace e2e; table Answer { value:int; }\n')
        self.node(self.graph(), "answer.fbs.h")

    def test_lex_include_tokens_require_a_closed_delimiter(self):
        self.write("ya.make", f"LIBRARY()\nADDINCL({PROJECT})\nSRCS(answer.l)\nEND()\n")
        self.write("answer.l", '''%option noyywrap
%{
#include
#include dummy wrong "missing.h"
#include "unfinished
#include <value.h>;
%}
%%
. ;
%%
''')
        self.write("value.h", "#pragma once\n")
        self.assertIn(f"$(S)/{PROJECT}/value.h", self.node(self.graph(), "answer.l.cpp.o")["inputs"])
