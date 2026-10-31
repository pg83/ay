from tst.make.lib import MakeTest, PROJECT


class PythonPlanningContextsTest(MakeTest):
    def test_generated_python_sources_follow_declared_search_directory_order(self):
        self.write("generate.py", 'import pathlib, sys\noutput = pathlib.Path(sys.argv[1])\noutput.parent.mkdir(parents=True, exist_ok=True)\noutput.write_text("VALUE = 42\\n")\n')
        for module, suffix in (("PY2_LIBRARY", ".yapyc"), ("PY3_LIBRARY", ".yapyc3")):
            with self.subTest(module=module):
                self.write("ya.make", f"{module}()\nRUN_PYTHON3(generate.py first/value.py OUT_NOAUTO first/value.py)\nRUN_PYTHON3(generate.py second/value.py OUT_NOAUTO second/value.py)\nSRCDIR(${{ARCADIA_BUILD_ROOT}}/{PROJECT}/first ${{ARCADIA_BUILD_ROOT}}/{PROJECT}/second)\nPY_SRCS(value.py)\nEND()\n")
                graph = self.graph()
                first = self.node(graph, "first/value.py")
                compiled = [node for node in graph["graph"] if f"$(B)/{PROJECT}/first/value.py" in node["inputs"] and any(output.endswith(suffix) for output in node["outputs"])]
                self.assertEqual(len(compiled), 1)
                self.assertIn(first["uid"], compiled[0]["deps"])
                self.assertNotIn(f"$(B)/{PROJECT}/second/value.py", compiled[0]["inputs"])
