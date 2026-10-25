from tst.make.lib import MakeTest, PROJECT


class PythonTest(MakeTest):
    def test_recursive_python_sources(self):
        self.write("ya.make", "PY3_LIBRARY()\nALL_PY_SRCS(RECURSIVE NAMESPACE e2e)\nEND()\n")
        self.write("module.py", "VALUE = 42\n")
        self.write("package/__init__.py", "\n")
        self.write("package/helper.py", "VALUE = 137\n")
        graph = self.graph()
        inputs = {path for node in graph["graph"] for path in node["inputs"]}
        for source in ("module.py", "package/__init__.py", "package/helper.py"):
            self.assertIn(f"$(S)/{PROJECT}/{source}", inputs)
