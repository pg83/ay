from tst.make.lib import MakeTest, PROJECT


class CgoDomainScanContextTest(MakeTest):
    def test_exported_c_headers_are_kept_after_an_earlier_domain_schema_scan(self):
        config = (self.root / "build/sysincl/misc.yml").read_text()
        self.write_tree("build/sysincl/misc.yml", config.replace("- includes:\n  - _cgo_export.h\n", ""))
        self.write("ya.make", "RECURSE(schema go)\n")
        self.write("schema/ya.make", "LIBRARY(schema)\nSRCS(answer.sc)\nEND()\n")
        self.write("schema/answer.sc", f'include "{PROJECT}/go/native.c";\n')
        self.write("go/ya.make", f"GO_LIBRARY(answer)\nADDINCL(${{ARCADIA_BUILD_ROOT}}/{PROJECT}/go)\nCGO_SRCS(answer.go)\nSRCS(CGO_EXPORT native.c)\nEND()\n")
        self.write("go/answer.go", 'package answer\nimport "C"\n//export Answer\nfunc Answer() C.int { return 42 }\n')
        self.write("go/native.c", '#include "_cgo_export.h"\nint native_answer(void) { return 42; }\n')
        graph = self.graph()
        exported = self.node(graph, "go/_cgo_export.h")
        compiled = self.node(graph, "go/native.c.o")
        self.assertIn(f"$(B)/{PROJECT}/go/_cgo_export.h", compiled["inputs"])
        self.assertIn(exported["uid"], compiled["deps"])
