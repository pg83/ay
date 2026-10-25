from tst.make.lib import MakeTest, PROJECT


class PythonVariantsTest(MakeTest):
    def test_python_two_program(self):
        self.write("ya.make", "PY2_PROGRAM(answer)\nPY_SRCS(MAIN main.py)\nEND()\n")
        self.write("main.py", "print(42)\n")
        self.node(self.graph(), "answer")

    def test_main_constructor_and_namespace(self):
        self.write("ya.make", "PY3_PROGRAM(answer)\nPY_SRCS(NAMESPACE e2e main.py init.py)\nPY_MAIN(e2e.main:main)\nPY_CONSTRUCTOR(e2e.init:initialize)\nNO_CHECK_IMPORTS(optional.*)\nEND()\n")
        self.write("main.py", "def main():\n    print(42)\n")
        self.write("init.py", "def initialize():\n    pass\n")
        self.node(self.graph("-t"), "answer")

    def test_python_generated_source(self):
        self.write("ya.make", "PY3_PROGRAM(answer)\nRUN_PYTHON3(generate.py OUT generated.py)\nPY_SRCS(generated.py)\nPY_MAIN(generated:main)\nEND()\n")
        self.write("generate.py", 'from pathlib import Path\nPath("generated.py").write_text("def main():\\n    print(42)\\n")\n')
        graph = self.graph()
        self.node(graph, "generated.py")
        self.node(graph, "answer")

    def test_recursive_pytest_sources(self):
        self.write("ya.make", "PY3TEST()\nALL_PYTEST_SRCS(RECURSIVE)\nEND()\n")
        self.write("test_answer.py", "def test_answer():\n    assert 42 == 6 * 7\n")
        self.write("nested/test_other.py", "def test_other():\n    assert True\n")
        graph = self.graph("-t")
        self.assertTrue(any(f"$(S)/{PROJECT}/nested/test_other.py" in node["inputs"] for node in graph["graph"]))
