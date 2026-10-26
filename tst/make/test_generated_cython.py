from tst.make.lib import MakeTest, PROJECT


class GeneratedCythonTest(MakeTest):
    def test_program_links_cython_objects_through_its_library(self):
        self.write("ya.make", "PY3_PROGRAM(answer)\nPY_SRCS(__main__.py value.pyx)\nEND()\n")
        self.write("__main__.py", "from .value import answer\nprint(answer())\n")
        self.write("value.pyx", "def answer(): return 42\n")
        graph = self.graph()
        obj = f"$(B)/{PROJECT}/value.pyx.cpp.o"
        self.node(graph, "value.pyx.cpp.o")
        self.assertTrue(any(obj in node["inputs"] and any(path.endswith(".a") for path in node["outputs"]) for node in graph["graph"]))
        self.assertNotIn(obj, self.node(graph, "answer")["inputs"])

    def test_forward_run_program_source_and_output_includes(self):
        self.write("generator/ya.make", "PY3_PROGRAM(generator)\nPY_SRCS(MAIN main.py)\nEND()\n")
        self.write("generator/main.py", 'import pathlib, sys\npathlib.Path(sys.argv[1]).parent.mkdir(parents=True, exist_ok=True)\npathlib.Path(sys.argv[1]).write_text("cimport dependency\\ndef answer(): return 42\\n")\n')
        self.write("dependency.pxd", "cdef int value\n")
        self.write("dependency.h", '#include "leaf.h"\n')
        self.write("leaf.h", "#pragma once\n")
        for module, suffix in (("PY2_LIBRARY", ".py2.cpp"), ("PY3_LIBRARY", ".cpp")):
            with self.subTest(module=module):
                self.write("ya.make", f"{module}()\nADDINCL(FOR cython {PROJECT})\nADDINCL({PROJECT})\nPY_SRCS(generated/answer.pyx)\nRUN_PROGRAM({PROJECT}/generator generated/answer.pyx OUT_NOAUTO generated/answer.pyx OUTPUT_INCLUDES dependency.pxd dependency.h)\nEND()\n")
                graph = self.graph()
                producer = self.node(graph, "generated/answer.pyx")
                consumer = self.node(graph, "generated/answer.pyx" + suffix)
                self.assertIn(f"$(B)/{PROJECT}/generated/answer.pyx", consumer["inputs"])
                self.assertIn(f"$(S)/{PROJECT}/dependency.pxd", consumer["inputs"])
                self.assertIn(producer["uid"], consumer["deps"])
                compiled_nodes = [node for node in graph["graph"] if f"$(B)/{PROJECT}/generated/answer.pyx" + suffix in node["inputs"] and any(path.endswith(".o") for path in node["outputs"])]
                self.assertEqual(len(compiled_nodes), 1)
                compiled = compiled_nodes[0]
                self.assertIn(f"$(S)/{PROJECT}/dependency.h", compiled["inputs"])
                self.assertIn(f"$(S)/{PROJECT}/leaf.h", compiled["inputs"])

    def test_output_includes_are_consumed_by_their_language(self):
        self.write("generator/ya.make", "PY3_PROGRAM(generator)\nPY_SRCS(MAIN main.py)\nEND()\n")
        self.write("generator/main.py", "print('generator')\n")
        self.write("dependency.pxd", "cdef int value\n")
        self.write("dependency.h", '#include "leaf.h"\n')
        self.write("leaf.h", "#pragma once\n")
        for module, suffix in (("PY2_LIBRARY", ".py2.cpp"), ("PY3_LIBRARY", ".cpp")):
            with self.subTest(module=module):
                self.write("ya.make", f"{module}()\nADDINCL(FOR cython {PROJECT})\nADDINCL({PROJECT})\nPY_SRCS(answer.pyx)\nRUN_PROGRAM({PROJECT}/generator OUT_NOAUTO answer.pyx OUTPUT_INCLUDES dependency.pxd dependency.h)\nEND()\n")
                graph = self.graph()
                consumer = self.node(graph, "answer.pyx" + suffix)
                self.assertIn(f"$(S)/{PROJECT}/dependency.pxd", consumer["inputs"])
                self.assertNotIn(f"$(S)/{PROJECT}/dependency.h", consumer["inputs"])
                self.assertNotIn(f"$(S)/{PROJECT}/leaf.h", consumer["inputs"])
                compiled_nodes = [node for node in graph["graph"] if f"$(B)/{PROJECT}/answer.pyx" + suffix in node["inputs"] and any(path.endswith(".o") for path in node["outputs"])]
                self.assertEqual(len(compiled_nodes), 1)
                compiled = compiled_nodes[0]
                self.assertIn(f"$(S)/{PROJECT}/dependency.h", compiled["inputs"])
                self.assertNotIn(f"$(S)/{PROJECT}/dependency.pxd", compiled["inputs"])
