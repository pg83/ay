import json

from tst.make.lib import MakeTest


class EmitterGraphOptionsTest(MakeTest):
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
