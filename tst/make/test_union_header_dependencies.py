from tst.make.lib import MakeTest, PROJECT


class UnionHeaderDependenciesTest(MakeTest):
    def copied(self, graph):
        matches = [node for node in graph["graph"] if node["outputs"] == [f"$(B)/{PROJECT}/packaged.h"]]
        self.assertEqual(len(matches), 1)
        return matches[0]

    def test_plain_copies_in_unions_keep_induced_headers_out_of_direct_inputs(self):
        self.write("generate.py", "print('#pragma once')\n")
        self.write("dependency.h", "#pragma once\n")
        for kind in ("h", "cpp"):
            with self.subTest(kind=kind):
                self.write("ya.make", f"UNION()\nPREPARE_INDUCED_DEPS(DEPS {kind} ${{ARCADIA_ROOT}}/{PROJECT}/dependency.h)\nRUN_PYTHON3(generate.py OUT_NOAUTO generated.h OUTPUT_INCLUDES missing.h INDUCED_DEPS $DEPS)\nCOPY_FILE(${{BINDIR}}/generated.h packaged.h)\nEND()\n")
                inputs = self.copied(self.graph())["inputs"]
                self.assertIn(f"$(B)/{PROJECT}/generated.h", inputs)
                self.assertNotIn(f"$(S)/{PROJECT}/dependency.h", inputs)

    def test_union_preserves_the_context_of_a_copied_header_template(self):
        self.write("ya.make", "UNION()\nCOPY_FILE_WITH_CONTEXT(template.h.in generated.h)\nCOPY_FILE(${BINDIR}/generated.h packaged.h)\nEND()\n")
        self.write("template.h.in", "#pragma once\n")
        self.assertIn(f"$(S)/{PROJECT}/template.h.in", self.copied(self.graph())["inputs"])
