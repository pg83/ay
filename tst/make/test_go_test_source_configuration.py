from tst.make.lib import MakeTest, PROJECT


class GoTestSourceConfigurationTest(MakeTest):
    def test_included_source_configuration_does_not_recurse_into_source_module_children(self):
        self.write("ya.make", "RECURSE(tests)\n")
        self.write("tests/ya.make", f"GO_TEST_FOR({PROJECT}/lib)\nEND()\n")
        self.write("lib/ya.make", f"SET(E2E_OPTIONS ${{ARCADIA_ROOT}}/{PROJECT}/options.inc)\nINCLUDE(${{E2E_OPTIONS}})\nGO_LIBRARY(answer)\nIF(E2E_FIRST_SOURCE)\nSRCS(first.go)\nELSE()\nSRCS(second.go)\nENDIF()\nGO_TEST_SRCS(answer_test.go)\nEND()\nRECURSE(missing)\n")
        self.write("lib/first.go", "package answer\nfunc Answer() int { return 42 }\n")
        self.write("lib/second.go", "package answer\nfunc Answer() int { return 42 }\n")
        self.write("lib/answer_test.go", 'package answer\nimport "testing"\nfunc TestAnswer(t *testing.T) { if Answer() != 42 { t.Fatal("wrong answer") } }\n')
        for value, source in (("yes", "first.go"), ("no", "second.go")):
            with self.subTest(source=source):
                self.write("options.inc", f"SET(E2E_FIRST_SOURCE {value})\n")
                graph = self.graph("-t")
                self.node(graph, "tests/test-results/answer/meta.json")
                self.assertTrue(any(f"$(S)/{PROJECT}/lib/{source}" in node["inputs"] for node in graph["graph"]))
