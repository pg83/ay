import base64
import hashlib

from tst.make.lib import MakeTest, PROJECT


class PythonCodegenTest(MakeTest):
    def test_mixed_cython_modes_preserve_each_sources_outputs(self):
        modes = (
            ("CYTHON_CPP_API_H", "cpp_api", ".cpp"),
            ("CYTHON_C_API_H", "c_api", ".c"),
            ("CYTHON_CPP_H", "cpp_header", ".cpp"),
            ("CYTHON_C_H", "c_header", ".c"),
            ("CYTHON_CPP", "cpp_plain", ".cpp"),
            ("CYTHON_C", "c_plain", ".c"),
        )
        sources = " ".join(f"{mode} {name}.pyx" for mode, name, _ in modes)
        self.write("ya.make", f"PY3_LIBRARY()\nPY_SRCS({sources})\nEND()\n")
        for _, name, _ in modes:
            self.write(name + ".pyx", "cdef public int answer():\n    return 42\ncdef api int api_answer():\n    return 42\n")
        graph = self.graph()
        for mode, name, suffix in modes:
            with self.subTest(mode=mode):
                output = name + ("" if mode.endswith("_H") else ".pyx") + suffix
                generated = self.node(graph, output)
                self.assertIn(f"$(S)/{PROJECT}/{name}.pyx", generated["inputs"])
                if mode.endswith("_H"):
                    self.assertIn(f"$(B)/{PROJECT}/{name}.h", generated["outputs"])
                if mode.endswith("_API_H"):
                    self.assertIn(f"$(B)/{PROJECT}/{name}_api.h", generated["outputs"])
                compiled = self.node(graph, output + ".o")
                self.assertIn(generated["uid"], compiled["deps"])

    def test_type_stub_uses_source_directory(self):
        self.write("ya.make", f"PY3_LIBRARY()\nSRCDIR({PROJECT}/src)\nPY_SRCS(answer.pyi)\nEND()\n")
        self.write("src/answer.pyi", "def answer() -> int: ...\n")
        graph = self.graph()
        self.assertTrue(any(f"$(S)/{PROJECT}/src/answer.pyi" in node["inputs"] for node in graph["graph"]))

    def test_fbs_python_source_name_keeps_arcadia_root(self):
        self.write("ya.make", f"PY3_PROGRAM(answer)\nPY_SRCS(MAIN main.py)\nPEERDIR({PROJECT}/schema/py)\nEND()\n")
        self.write("main.py", "print(42)\n")
        self.write("schema/py/ya.make", f"FBS_LIBRARY()\nSRCS(${{ARCADIA_ROOT}}/{PROJECT}/schema/answer.fbs)\nEND()\n")
        self.write("schema/answer.fbs", "namespace e2e;\ntable Answer { value: int; }\nroot_type Answer;\n")
        # pybuild names the archive listid([(path, module)]) of the PY_SRCS
        # argument as written, ${ARCADIA_ROOT} included.
        path = f"${{ARCADIA_ROOT}}/{PROJECT}/schema/answer"
        items = [(path + ".fbs", f"{PROJECT}.schema.py." + path.replace("/", "."))]
        name = base64.b32encode(hashlib.md5(str(items).encode()).digest()).decode().lower().rstrip("=")
        self.node(self.graph(), f"schema/py/{name}.py3.fbs.pysrc")

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

    def test_python_source_shadows_stdout_noauto_output(self):
        self.write("ya.make", "PY3_LIBRARY()\nRUN_PYTHON3(generate.py STDOUT_NOAUTO answer.py)\nPY_SRCS(answer.py)\nEND()\n")
        self.write("generate.py", 'print("VALUE = 42")\n')
        self.write("answer.py", "VALUE = 42\n")
        graph = self.graph()
        compiled = [node for node in graph["graph"] if any(path.startswith(f"$(B)/{PROJECT}/answer.py.") and path.endswith(".yapyc3") for path in node["outputs"])]
        self.assertEqual(len(compiled), 1)
        self.assertIn(f"$(S)/{PROJECT}/answer.py", compiled[0]["inputs"])
        self.assertNotIn(f"$(B)/{PROJECT}/answer.py", compiled[0]["inputs"])
        self.assertFalse([node for node in graph["graph"] if f"$(B)/{PROJECT}/answer.py" in node["outputs"]])
