import json

from tst.make.lib import MakeTest, PROJECT


class EmitterGraphOptionsTest(MakeTest):
    def test_keep_going_with_ownership_checks_preserves_healthy_modules(self):
        self.write("ya.make", "RECURSE(broken healthy)\n")
        self.write("broken/ya.make", "LIBRARY()\nRUN_PYTHON3(generate.py IN ${BINDIR}/second.cpp OUT first.cpp)\nRUN_PYTHON3(generate.py IN ${BINDIR}/first.cpp OUT second.cpp)\nEND()\n")
        self.write("broken/generate.py", "pass\n")
        self.write("healthy/ya.make", "PROGRAM(answer)\nSRCS(main.cpp)\nEND()\n")
        self.write("healthy/main.cpp", "int main() { return 0; }\n")
        result = self.make("-k", env={"AY_DEBUG_OWNERSHIP": "1"})
        self.assertEqual(result.returncode, 0, result.stderr)
        graph = json.loads(result.stdout)
        roots = [output for node in graph["graph"] if node["uid"] in graph["result"] for output in node["outputs"]]
        self.assertTrue(any(output.endswith(f"/{PROJECT}/healthy/answer") for output in roots), roots)

    def test_non_sandboxed_graph_keeps_source_and_generated_dependencies(self):
        self.write("ya.make", "LIBRARY()\nRUN_PYTHON3(generate.py STDOUT generated.cpp)\nEND()\n")
        self.write("generate.py", 'print("int answer() { return 42; }")\n')
        result = self.make(sandboxing=False)
        self.assertEqual(result.returncode, 0, result.stderr)
        graph = json.loads(result.stdout)
        self.assertTrue(graph["result"])
        generated = next(node for node in graph["graph"] if any(path.endswith(f"/{PROJECT}/generated.cpp") for path in node["outputs"]))
        compiled = next(node for node in graph["graph"] if any(path.endswith(f"/{PROJECT}/generated.cpp.o") for path in node["outputs"]))
        self.assertIn(generated["uid"], compiled["deps"])
        self.assertTrue(any(path.endswith(f"/{PROJECT}/generated.cpp") for path in compiled["inputs"]))
        self.assertTrue(any(path.endswith(f"/{PROJECT}/generate.py") for path in generated["inputs"]))

    def test_cyclic_generator_inputs_are_rejected(self):
        self.write("ya.make", "LIBRARY()\nRUN_PYTHON3(generate.py IN ${BINDIR}/second.cpp OUT first.cpp)\nRUN_PYTHON3(generate.py IN ${BINDIR}/first.cpp OUT second.cpp)\nEND()\n")
        self.write("generate.py", "pass\n")
        for debug in ("", "1"):
            with self.subTest(debug=debug):
                self.assertNotEqual(self.make(env={"AY_DEBUG_PENDING": debug}).returncode, 0)

    def test_stripping_packages_keeps_an_independent_program(self):
        self.write("ya.make", "RECURSE(package app)\n")
        self.write("package/ya.make", "PACKAGE(data)\nFILES(value.txt)\nEND()\n")
        self.write("package/value.txt", "42\n")
        self.write("app/ya.make", "PROGRAM(answer)\nSRCS(main.cpp)\nEND()\n")
        self.write("app/main.cpp", "int main() { return 0; }\n")
        graph = self.graph(env={"YA_STRIP_PACKAGES_FROM_RESULTS": "1"})
        self.assertEqual(graph["result"], [self.node(graph, "app/answer")["uid"]])
        self.assertFalse(any(path.endswith(".final.pkg.fake") for node in graph["graph"] for path in node["outputs"]))

    def test_stripping_a_standalone_package_leaves_no_results(self):
        self.write("ya.make", "PACKAGE(data)\nFILES(value.txt)\nEND()\n")
        self.write("value.txt", "42\n")
        result = self.make(env={"YA_STRIP_PACKAGES_FROM_RESULTS": "1"})
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(json.loads(result.stdout)["result"], [])
