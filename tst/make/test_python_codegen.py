from tst.make.lib import MakeTest, PROJECT


class PythonCodegenTest(MakeTest):
    def test_cython_output_modes(self):
        for mode in ("CYTHON_C", "CYTHON_CPP", "CYTHON_C_H", "CYTHON_CPP_H", "CYTHON_C_API_H", "CYTHON_CPP_API_H"):
            with self.subTest(mode=mode):
                self.write("ya.make", f"PY3_LIBRARY()\nPY_SRCS({mode} CYTHON_DIRECTIVE boundscheck=False answer.pyx)\nEND()\n")
                self.write("answer.pyx", "cdef public int answer():\n    return 42\ncdef api int api_answer():\n    return 42\n")
                graph = self.graph()
                self.assertTrue(any(f"$(S)/{PROJECT}/answer.pyx" in node["inputs"] and any(path.endswith((".c", ".cpp")) for path in node["outputs"]) for node in graph["graph"]))

    def test_cythonize_python(self):
        self.write("ya.make", "PY3_LIBRARY()\nPY_SRCS(TOP_LEVEL CYTHONIZE_PY answer.py)\nEND()\n")
        self.write("answer.py", "def answer():\n    return 42\n")
        self.write("answer.pxd", "cpdef int answer()\n")
        graph = self.graph()
        self.assertTrue(any(f"$(S)/{PROJECT}/answer.py" in node["inputs"] and any(path.endswith(".cpp") for path in node["outputs"]) for node in graph["graph"]))

    def test_swig_python_modes(self):
        for mode in ("SWIG_C", "SWIG_CPP"):
            with self.subTest(mode=mode):
                self.write("ya.make", f"PY3_LIBRARY()\nPY_SRCS({mode} answer.swg)\nEND()\n")
                self.write("answer.swg", '%module answer\n%inline %{ int answer(void) { return 42; } %}\n')
                graph = self.graph()
                self.assertTrue(any(f"$(S)/{PROJECT}/answer.swg" in node["inputs"] and any(path.endswith((".c", ".cpp")) for path in node["outputs"]) for node in graph["graph"]))

    def test_python_type_stubs_and_explicit_namespace(self):
        self.write("ya.make", "PY3_LIBRARY()\nPY_SRCS(NAMESPACE e2e answer.py=custom.answer answer.pyi=custom.answer)\nEND()\n")
        self.write("answer.py", "def answer():\n    return 42\n")
        self.write("answer.pyi", "def answer() -> int: ...\n")
        graph = self.graph()
        self.assertTrue(any(f"$(S)/{PROJECT}/answer.pyi" in node["inputs"] for node in graph["graph"]))
