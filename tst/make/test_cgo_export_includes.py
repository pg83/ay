from tst.make.lib import MakeTest, PROJECT


class CgoExportIncludesTest(MakeTest):
    def fixture(self, include):
        self.write("ya.make", "GO_LIBRARY(answer)\nCGO_SRCS(answer.go)\nSRCS(CGO_EXPORT native.c)\nEND()\n")
        self.write("answer.go", 'package answer\nimport "C"\n//export Answer\nfunc Answer() C.int { return 42 }\n')
        self.write("native.c", f"#include {include}\nint native_answer(void) {{ return 42; }}\n")

    def test_exported_c_source_can_include_its_generated_header(self):
        self.fixture('"_cgo_export.h"')
        graph = self.graph()
        exported = self.node(graph, "_cgo_export.h")
        compiled = self.node(graph, "native.c.o")
        self.assertIn(f"$(B)/{PROJECT}/_cgo_export.h", compiled["inputs"])
        self.assertIn(exported["uid"], compiled["deps"])

    def test_unprovided_quoted_headers_are_rejected(self):
        self.fixture('"missing.h"')
        result = self.make()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("missing.h", result.stderr)

    def test_unprovided_system_headers_are_rejected(self):
        self.fixture("<missing.h>")
        result = self.make()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("missing.h", result.stderr)
