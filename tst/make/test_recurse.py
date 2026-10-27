import json

from tst.make.lib import MakeTest, PROJECT


class RecurseTest(MakeTest):
    def test_nested_directories_without_modules(self):
        self.write("ya.make", "RECURSE(group)\n")
        self.write("group/ya.make", f"RECURSE(app)\nRECURSE_ROOT_RELATIVE({PROJECT}/group/app)\n")
        self.write("group/app/ya.make", "PROGRAM(answer)\nSRCS(main.cpp)\nEND()\n")
        self.write("group/app/main.cpp", "int main() { return 0; }\n")
        graph = self.graph()
        program = self.node(graph, "group/app/answer")
        self.assertIn(program["uid"], graph["result"])
        self.assertEqual(graph["result"].count(program["uid"]), 1)

    def test_directory_without_selected_modules(self):
        self.write("ya.make", "SET(BUILD_PROGRAM no)\nIF (BUILD_PROGRAM)\nPROGRAM(answer)\nSRCS(main.cpp)\nEND()\nENDIF()\n")
        self.write("main.cpp", "int main() { return 0; }\n")
        result = self.make()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(json.loads(result.stdout)["result"], [])

    def test_recursed_library_is_a_start_target(self):
        self.write("ya.make", "RECURSE(lib)\n")
        self.write("lib/ya.make", "LIBRARY(answer)\nSRCS(answer.cpp)\nEND()\n")
        self.write("lib/answer.cpp", "int answer() { return 42; }\n")
        graph = self.graph()
        library = self.node(graph, "lib/libanswer.a")
        self.assertIn(library["uid"], graph["result"])

    def test_strict_build_exclusion_discards_directory_recurses(self):
        self.write("ya.make", "RECURSE(group kept)\n")
        self.write("group/answer.cpp", "int answer() { return 42; }\n")
        for name in ("app", "tests", "kept"):
            directory = name if name == "kept" else f"group/{name}"
            self.write(f"{directory}/ya.make", "PY3TEST()\nTEST_SRCS(test_answer.py)\nEND()\n")
            self.write(f"{directory}/test_answer.py", "def test_answer():\n    assert True\n")
        for mode, discarded in (("STRICT", True), ("WARNING", False)):
            with self.subTest(mode=mode):
                self.write("group/discard.inc", f"LIBRARY(group)\nNO_BUILD_IF({mode} E2E_DISCARD)\nSRCS(answer.cpp)\nEND()\n")
                self.write("group/ya.make", f"SET(E2E_DISCARD yes)\nRECURSE(app)\nRECURSE_FOR_TESTS(tests)\nINCLUDE(${{ARCADIA_ROOT}}/{PROJECT}/group/discard.inc)\n")
                graph = self.graph("-t")
                self.node(graph, "kept/test-results/py3test/meta.json")
                outputs = {path for node in graph["graph"] for path in node["outputs"]}
                for output in ("group/app/test-results/py3test/meta.json", "group/tests/test-results/py3test/meta.json"):
                    self.assertEqual(f"$(B)/{PROJECT}/{output}" not in outputs, discarded, output)
