from tst.make.lib import MakeTest, PROJECT


class ConfigurationRecursesTest(MakeTest):
    def test_test_dependencies_do_not_select_recursed_test_suites(self):
        self.write("ya.make", f"PY3TEST()\nTEST_SRCS(test_answer.py)\nPEERDIR({PROJECT}/lib)\nDEPENDS({PROJECT}/lib)\nEND()\n")
        self.write("test_answer.py", "def test_answer(): assert True\n")
        self.write("lib/ya.make", "PY3_LIBRARY()\nPY_SRCS(answer.py)\nEND()\nRECURSE(./nested/../nested)\n")
        self.write("lib/answer.py", "ANSWER = 42\n")
        self.write("lib/nested/ya.make", "PY3TEST()\nTEST_SRCS(test_nested.py)\nEND()\n")
        self.write("lib/nested/test_nested.py", "def test_nested(): assert True\n")
        graph = self.graph("-t", "--add-modules-to-results")
        self.node(graph, "test-results/py3test/meta.json")
        nested_meta = f"$(B)/{PROJECT}/lib/nested/test-results/py3test/meta.json"
        self.assertFalse(any(nested_meta in node["outputs"] for node in graph["graph"]))
