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
