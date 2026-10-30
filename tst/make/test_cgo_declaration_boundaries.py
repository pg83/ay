from tst.make.lib import MakeTest, PROJECT


class CgoDeclarationBoundariesTest(MakeTest):
    def test_source_rooted_cgo_sources_are_rejected(self):
        self.write("ya.make", f"GO_LIBRARY(answer)\nCGO_SRCS(${{ARCADIA_ROOT}}/{PROJECT}/answer.go)\nEND()\n")
        self.write("answer.go", 'package answer\nimport "C"\nfunc Answer() C.int { return 42 }\n')
        result = self.make()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("answer.cgo2.c", result.stderr)

    def test_exported_c_source_requires_the_cgo_export_header(self):
        self.write("ya.make", "GO_LIBRARY(answer)\nSRCS(answer.go CGO_EXPORT native.c)\nEND()\n")
        self.write("answer.go", "package answer\nconst Answer = 42\n")
        self.write("native.c", '#include "native.h"\nint native_answer(void) { return VALUE; }\n')
        self.write("native.h", "#define VALUE 42\n")
        result = self.make()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("_cgo_export.h", result.stderr)
