from tst.make.lib import MakeTest, PROJECT


class NativeCythonSourcesTest(MakeTest):
    def check_native_source(self, python3, output):
        headers = "PYTHON3_ADDINCL()" if python3 == "yes" else "USE_PYTHON2()\nPYTHON2_ADDINCL()"
        self.write("ya.make", f"LIBRARY()\nSET(PYTHON3 {python3})\n{headers}\nSRCS(answer.pyx)\nEND()\n")
        self.write("answer.pyx", "def answer():\n    return 42\n")
        graph = self.graph()
        generated = self.node(graph, output)
        self.assertIn(f"$(S)/{PROJECT}/answer.pyx", generated["inputs"])
        compiled = self.node(graph, output + ".o")
        self.assertIn(f"$(B)/{PROJECT}/{output}", compiled["inputs"])
        self.assertIn(generated["uid"], compiled["deps"])

    def test_python3_native_cython_sources_are_compiled(self):
        self.check_native_source("yes", "answer.pyx.cpp")

    def test_python2_native_cython_sources_keep_the_versioned_output(self):
        self.check_native_source("no", "answer.pyx.py2.cpp")
