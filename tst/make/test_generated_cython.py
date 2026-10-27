from tst.make.lib import MakeTest, PROJECT


class GeneratedCythonTest(MakeTest):
    def test_python3_only_source_in_py23_module(self):
        self.write("ya.make", "PY23_LIBRARY()\nIF (PYTHON3)\nPY_SRCS(answer.pyx)\nENDIF()\nEND()\n")
        self.write("answer.pyx", "from libcpp.vector cimport vector\ndef answer(): return 42\n")
        node = self.node(self.graph(), "answer.pyx.py3.cpp")
        self.assertIn("$(S)/contrib/tools/cython/Cython/Includes/libcpp/vector.pxd", node["inputs"])
        self.assertNotIn("$(S)/contrib/tools/cython_py2/Cython/Includes/libcpp/vector.pxd", node["inputs"])

    def test_parenthesized_cimport_with_comment(self):
        self.write("ya.make", "PY3_LIBRARY()\nPY_SRCS(answer.pyx)\nEND()\n")
        self.write("dependency.pxd", "cdef int value\n")
        for comment in ("", "    # ignored name\n"):
            with self.subTest(comment=bool(comment)):
                self.write("answer.pyx", f"from dependency cimport (\n{comment}    value,\n)\ndef answer(): return 42\n")
                node = self.node(self.graph(), "answer.pyx.cpp")
                self.assertEqual(f"$(S)/{PROJECT}/dependency.pxd" in node["inputs"], not comment)

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

    def args(self, node):
        return [arg.replace("$(SOURCE_ROOT)", "$(S)").replace("$(BUILD_ROOT)", "$(B)") for command in node["cmds"] for arg in command["cmd_args"]]

    def test_buildwith_cython_output_names(self):
        # BUILDWITH_CYTHON_C/CPP name the output ${OBJ_SUF}${CYTHON_SUFFIX}.c(pp)
        # after the source; CYTHON_SUFFIX is .py2 unless the module is PYTHON3.
        # A PY2_PROGRAM consumer selects the python 2 variant of a multimodule.
        self.write("py2/ya.make", f"PY2_PROGRAM(answer)\nPY_SRCS(MAIN main.py)\nPEERDIR({PROJECT}/lib)\nEND()\n")
        self.write("py2/main.py", "print(42)\n")
        self.write("lib/answer.pyx", "def answer():\n    return 42\n")
        for module, consumers, objects in (
            ("PY3_LIBRARY", "lib", {"answer.pyx{ext}": ""}),
            ("PY2_LIBRARY", "lib", {"answer.pyx.py2{ext}": ""}),
            ("PY23_LIBRARY", "lib py2", {"answer.pyx.py3{ext}": ".py3", "answer.pyx.py2.py2{ext}": ".py2"}),
            ("PY23_NATIVE_LIBRARY", "lib py2", {"answer.pyx.py3{ext}": ".py3", "answer.pyx.py2.py2{ext}": ".py2"}),
        ):
            for macro, ext in (("BUILDWITH_CYTHON_C", ".c"), ("BUILDWITH_CYTHON_CPP", ".cpp")):
                with self.subTest(module=module, macro=macro):
                    self.write("ya.make", f"RECURSE({consumers})\n")
                    self.write("lib/ya.make", f"{module}()\n{macro}(answer.pyx)\nEND()\n")
                    graph = self.graph()
                    produced = sorted(path.removeprefix(f"$(B)/{PROJECT}/lib/") for node in graph["graph"] for path in node["outputs"] if f"$(S)/{PROJECT}/lib/answer.pyx" in node["inputs"])
                    self.assertEqual(produced, sorted(output.format(ext=ext) for output in objects))
                    for output, suffix in objects.items():
                        self.node(graph, f"lib/{output.format(ext=ext)}{suffix}.o")

    def test_cython_sys_includes_join_module_addincl(self):
        # ymake merges every ADDINCL call of a module into one statement in
        # call order (the one in BUILDWITH_CYTHON_C included) and runs it
        # before ADDINCLSELF, whatever the order in ya.make.
        self.write("answer.pyx", "def answer():\n    return 42\n")
        self.write("value.c", "int value(void) { return 42; }\n")
        self.write("first/first.h", "#pragma once\n")
        self.write("second/second.h", "#pragma once\n")
        first, second, here = (f"-I$(S)/{PROJECT}/first", f"-I$(S)/{PROJECT}/second", f"-I$(S)/{PROJECT}")
        numpy = "-I$(S)/contrib/python/numpy/include/numpy/core/include"
        for body, expected in (
            (f"ADDINCL({PROJECT}/first)\nADDINCLSELF()\nBUILDWITH_CYTHON_C(answer.pyx)\nADDINCL({PROJECT}/second)\n", [first, numpy, second, here]),
            (f"BUILDWITH_CYTHON_C(answer.pyx)\nADDINCLSELF()\nADDINCL({PROJECT}/first)\n", [numpy, first, here]),
        ):
            with self.subTest(body=body):
                self.write("ya.make", f"PY3_LIBRARY()\n{body}SRCS(value.c)\nEND()\n")
                graph = self.graph()
                for output in ("value.c.o", "answer.pyx.c.o"):
                    args = self.args(self.node(graph, output))
                    self.assertEqual([arg for arg in args if arg in expected], expected, output)

    def test_python_macro_restrictions_across_variants(self):
        # A macro restricted in only some variants of a multimodule is ignored
        # in those: USE_PYTHON2 neither fails PY23_LIBRARY nor turns its
        # python 3 variant into a python 2 one (flags, registration, peers).
        self.write("ya.make", "RECURSE(py2 py3)\n")
        self.write("lib/ya.make", f"PY23_LIBRARY()\nUSE_PYTHON2()\nSRCS(answer.cpp)\nPY_REGISTER(answer)\nPEERDIR({PROJECT}/dep)\nEND()\n")
        self.write("lib/answer.cpp", "int answer() { return 42; }\n")
        self.write("dep/ya.make", "PY23_LIBRARY()\nSRCS(dep.cpp)\nEND()\n")
        self.write("dep/dep.cpp", "int dep() { return 42; }\n")
        for python in ("py2", "py3"):
            self.write(f"{python}/ya.make", f"{python.upper()}_PROGRAM(answer)\nPY_SRCS(MAIN main.py)\nPEERDIR({PROJECT}/lib)\nEND()\n")
            self.write(f"{python}/main.py", "print(42)\n")
        result = self.make()
        self.assertEqual(result.returncode, 0, result.stderr)
        graph = self.graph()
        for output, python2 in (("lib/answer.cpp.py3.o", False), ("lib/answer.cpp.py2.o", True)):
            args = self.args(self.node(graph, output))
            self.assertEqual("-DUSE_PYTHON2" in args, python2, output)
            self.assertEqual("-DUSE_PYTHON3" in args, not python2, output)
        self.node(graph, "lib/answer.reg3.cpp")
        self.node(graph, "lib/answer.reg.cpp")
        inputs = self.node(graph, "py3/answer")["inputs"]
        self.assertIn(f"$(B)/{PROJECT}/dep/libpy3{PROJECT}-dep.a", inputs)
        self.assertNotIn(f"$(B)/{PROJECT}/dep/libpy{PROJECT}-dep.a", inputs)
        # Restricted in every variant (or in a plain module) it is an error
        # and the call is skipped.
        self.write("ya.make", "RECURSE(lib)\n")
        for module, macro, output in (("PY3_LIBRARY", "USE_PYTHON2", "answer.cpp.o"), ("PY23_NATIVE_LIBRARY", "USE_PYTHON3", "answer.cpp.py3.o")):
            with self.subTest(module=module, macro=macro):
                self.write("lib/ya.make", f"{module}()\n{macro}()\nSRCS(answer.cpp)\nEND()\n")
                result = self.make()
                self.assertNotEqual(result.returncode, 0, result.stderr)
                self.assertIn(macro, result.stderr)
                args = self.args(self.node(self.graph("-k"), f"lib/{output}"))
                self.assertIn("-DUSE_PYTHON3", args)
                self.assertNotIn("-DUSE_PYTHON2", args)
