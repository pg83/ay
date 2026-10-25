from tst.make.lib import MakeTest, PROJECT


class ParserVariantsTest(MakeTest):
    def test_flatbuffers_generation_defers_syntax_validation(self):
        # -j0 builds the generator command; schema syntax is checked by flatc
        # only when that command executes. Both CLIs accept these inputs here.
        samples = (
            'R"tag(raw )other\ntext)tag";\n',
            'R"abcdefghijklmnopq(unfinished\n',
            "'a' '\\n' 'unterminated\n",
            '"escaped \\" quote"\n"unterminated\n',
            '# include "quoted\\\\file.h"\n#include_next <header.h>\n',
            '# other\n#include SYMBOL\n#include\n#\n   \n',
            '#include "missing-end\n#include <missing-end\n',
            '/* unfinished block comment\n',
            '// last line without newline',
        )
        for text in samples:
            with self.subTest(text=text):
                self.write("ya.make", "LIBRARY()\nSRCS(answer.fbs)\nEND()\n")
                self.write("answer.fbs", "namespace e2e; table Answer { value: int; }\n" + text)
                self.node(self.graph(), "answer.fbs.h")

    def test_compiler_variable_expressions(self):
        self.write("answer.cpp", "int answer() { return 42; }\n")
        macros = (
            'SET(FIRST -DFIRST=1)\nSET(SECOND $FIRST -DSECOND=2)\nCFLAGS($SECOND)',
            'SET(VALUE 42)\nCFLAGS(-DANSWER=${VALUE} -DEMPTY="" -DQUOTE=\'value\')',
            'SET(WORDS -DFIRST=1 -DSECOND=2)\nCFLAGS("$WORDS")',
            'CFLAGS(-DBACKSLASH=\\\\ -DUTF8=ответ -DPRICE=$5)',
            'CFLAGS(-DVALUE=${quo:"a b"} -DCOMMA=${comma:"ignored"})',
        )
        for macro in macros:
            with self.subTest(macro=macro):
                self.write("ya.make", f"LIBRARY()\n{macro}\nSRCS(answer.cpp)\nEND()\n")
                self.node(self.graph(), "answer.cpp.o")

    def test_include_cycles_and_line_endings(self):
        self.write("ya.make", f"LIBRARY()\nADDINCL({PROJECT}/include)\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", '# /* comment */ include "first.h"\r\n#include <second.h>\r\nint answer() { return 42; }')
        self.write("include/first.h", '#pragma once\r\n#include "second.h"\r\n')
        self.write("include/second.h", '#pragma once\r\n#include "first.h"')
        graph = self.graph()
        self.assertIn(f"$(S)/{PROJECT}/include/second.h", self.node(graph, "answer.cpp.o")["inputs"])

    def test_unterminated_quote(self):
        self.write("ya.make", 'LIBRARY()\nSRCS("unterminated)\nEND()\n')
        self.assertNotEqual(self.make().returncode, 0)

    def test_unused_partial_expressions(self):
        for suffix in ('SET(NAME ${unfinished)', 'SET(NAME "closed"tail)', 'IF(VALUE ==)\nENDIF()', 'IF(NOT)\nENDIF()', 'IF(VALUE AND)\nENDIF()', 'IF(VALUE OR)\nENDIF()'):
            with self.subTest(suffix=suffix):
                self.write("ya.make", f"LIBRARY()\n{suffix}\nSRCS(answer.cpp)\nEND()\n")
                self.write("answer.cpp", "int answer() { return 42; }\n")
                self.node(self.graph(), "answer.cpp.o")
