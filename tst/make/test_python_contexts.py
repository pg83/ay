import json

from tst.make.lib import MakeTest, PROJECT


class PythonContextsTest(MakeTest):
    def test_native_variants(self):
        self.write("answer.cpp", "int answer() { return 42; }\n")
        for declaration, macros in (("PY23_NATIVE_LIBRARY()", ""), ("PY2MODULE(answer)", ""), ("LIBRARY()", "USE_PYTHON2()\nPYTHON2_ADDINCL()"), ("LIBRARY()", "PYTHON3_ADDINCL()")):
            with self.subTest(declaration=declaration, macros=macros):
                self.write("ya.make", f"{declaration}\n{macros}\nSRCS(answer.cpp)\nEND()\n")
                graph = self.graph()
                self.assertTrue(any(f"$(S)/{PROJECT}/answer.cpp" in node["inputs"] for node in graph["graph"]))

    def test_import_masks(self):
        self.write("main.py", "print(42)\n")
        for macros in (
            "NO_CHECK_IMPORTS(optional.*)\nSET_APPEND(NO_CHECK_IMPORTS_FOR_VALUE another.*)",
            "SET(NO_CHECK_IMPORTS_FOR_VALUE first.* second.*)\nNO_CHECK_IMPORTS()",
            "SET(MASK optional.*)\nNO_CHECK_IMPORTS(${MASK})\nSET_APPEND(NO_CHECK_IMPORTS_FOR_VALUE prefix.${MASK})",
        ):
            with self.subTest(macros=macros):
                self.write("ya.make", f"PY3_PROGRAM(answer)\n{macros}\nPY_SRCS(MAIN main.py)\nEND()\n")
                self.graph("-t")

    def test_lint_extra_sources(self):
        sources = json.dumps([f"$S/{PROJECT}/seed.py", f"${{ARCADIA_ROOT}}/{PROJECT}/nested/seed.py"], separators=(",", ":"))
        self.write("ya.make", f"PY3_LIBRARY()\nSET(PY_LINTER_FILES {sources})\nPY_EXTRA_LINT_FILES(extra.py nested/other.py)\nPY_SRCS(answer.py)\nEND()\n")
        self.write("answer.py", "answer = 42\n")
        self.write("extra.py", "extra = 17\n")
        self.write("nested/other.py", "other = 12\n")
        self.write("seed.py", "seed = 17\n")
        self.write("nested/seed.py", "seed = 12\n")
        graph = self.graph("-t")
        commands = json.dumps([node.get("cmds", []) for node in graph["graph"]])
        for file in ("extra.py", "nested/other.py", "seed.py", "nested/seed.py"):
            self.assertIn(f"/{PROJECT}/{file}", commands)

    def test_absolute_python_source_is_rejected(self):
        self.write("ya.make", f"PY3_LIBRARY()\nPY_SRCS(TOP_LEVEL ${{ARCADIA_ROOT}}/{PROJECT}/answer.py ./nested/other.py=other)\nEND()\n")
        self.write("answer.py", "answer = 42\n")
        self.write("nested/other.py", "other = 17\n")
        result = self.make()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("answer.py.", result.stderr)
        self.assertIn(".yapyc3", result.stderr)
        self.write("ya.make", f"PY3_LIBRARY()\nENABLE(PYBUILD_NO_PYC)\nPY_SRCS(TOP_LEVEL ${{ARCADIA_ROOT}}/{PROJECT}/answer.py ./nested/other.py=other)\nEND()\n")
        graph = self.graph()
        self.assertTrue(any(f"$(S)/{PROJECT}/answer.py" in node["inputs"] for node in graph["graph"]))
