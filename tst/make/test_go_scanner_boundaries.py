from tst.make.lib import MakeTest, PROJECT


class GoScannerBoundariesTest(MakeTest):
    def test_incomplete_import_lists_do_not_lose_the_source(self):
        self.write("ya.make", "GO_LIBRARY()\nSRCS(answer.go)\nEND()\n")
        for imports in ('import (', 'import (;', 'import 42', '/* unfinished'):
            with self.subTest(imports=imports):
                self.write("answer.go", "package answer\n" + imports)
                compiled = self.node(self.graph(), f"{PROJECT}.a")
                self.assertIn(f"$(S)/{PROJECT}/answer.go", compiled["inputs"])

    def test_empty_import_paths_are_rejected(self):
        self.write("ya.make", "GO_LIBRARY()\nSRCS(answer.go)\nEND()\n")
        self.write("answer.go", 'package answer\nimport (""')
        self.assertNotEqual(self.make().returncode, 0)

    def test_cgo_ignores_comment_markers_inside_escaped_and_raw_strings(self):
        self.write("ya.make", "GO_LIBRARY()\nCGO_SRCS(answer.go)\nEND()\n")
        self.write("answer.h", "int answer(void);\n")
        self.write("ignored.h", "int ignored(void);\n")
        self.write("answer.go", '''package answer
/*
#include "answer.h"
*/
import "C"
var quoted = "\\\" /* #include \\"ignored.h\\" */"
var raw = `
/*
#include "ignored.h"
*/
`
var quote = '\\''
func Answer() int { return int(C.answer()) }
''')
        generated = self.node(self.graph(), "answer.cgo1.go")
        self.assertIn(f"$(S)/{PROJECT}/answer.h", generated["inputs"])
        self.assertNotIn(f"$(S)/{PROJECT}/ignored.h", generated["inputs"])

    def test_cgo_ignores_header_dependencies_in_unterminated_comments(self):
        self.write("ya.make", "GO_LIBRARY()\nCGO_SRCS(answer.go)\nEND()\n")
        self.write("answer.h", "int answer(void);\n")
        self.write("answer.go", 'package answer\nimport "C"\n/*\n#include "answer.h"\n')
        generated = self.node(self.graph(), "answer.cgo1.go")
        self.assertNotIn(f"$(S)/{PROJECT}/answer.h", generated["inputs"])
