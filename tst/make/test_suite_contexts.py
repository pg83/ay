from tst.make.lib import MakeTest, PROJECT


class SuiteContextsTest(MakeTest):
    def test_yql_minitest_runtime_dependencies(self):
        self.write("ya.make", f"YQL_UDF_MINITEST(answer)\nDEPENDS({PROJECT}/udf)\nEND()\n")
        self.write("udf/ya.make", "YQL_UDF_CONTRIB(answer)\nSRCS(answer.cpp)\nEND()\n")
        self.write("udf/answer.cpp", 'extern "C" int answer() { return 42; }\n')
        graph = self.graph("-t")
        suite = self.node(graph, "test-results/py3test/meta.json")
        self.node(graph, "udf/libanswer.so")
        self.assertTrue(any("$(B)/yql/essentials/tools/minirun/minirun" in node["outputs"] for node in graph["graph"]))
        args = [arg for command in suite["cmds"] for arg in command["cmd_args"]]
        self.assertTrue(any("YQL_MINIRUN_PATH=yql/essentials/tools/minirun/minirun" in arg for arg in args))

    def test_python_test_bin_module(self):
        self.write("ya.make", "PY3TEST_BIN()\nTEST_SRCS(test_answer.py)\nEND()\n")
        self.write("test_answer.py", "def test_answer():\n    assert 42 == 42\n")
        graph = self.graph("-t")
        self.node(graph, "test_answer.py.yapyc3")
        self.node(graph, "test-results/py3test/meta.json")

    def test_ktlint_compatibility_excludes_external_source_directories(self):
        self.write("ya.make", "RECURSE(tests)\n")
        self.write("tests/ya.make", "JUNIT5(answer)\nWITH_YA_1931()\nWITH_KOTLIN()\nJDK_VERSION(21)\nJAVA_SRCS(SRCDIR ../src **/*)\nEND()\n")
        self.write("src/Answer.kt", "class Answer\n")
        graph = self.graph("-t")
        node = self.node(graph, "tests/test-results/ktlint/meta.json")
        self.assertNotIn(f"$(S)/{PROJECT}/src", node["inputs"])
        args = [arg.replace("$(SOURCE_ROOT)", "$(S)") for command in node["cmds"] for arg in command["cmd_args"]]
        related = [args[i + 1] for i, value in enumerate(args[:-1]) if value == "--test-related-path"]
        self.assertNotIn(f"$(S)/{PROJECT}/src", related)
