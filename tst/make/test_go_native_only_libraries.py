from tst.make.lib import MakeTest


class GoNativeOnlyLibrariesTest(MakeTest):
    def test_a_native_only_go_library_keeps_its_compiled_object(self):
        self.write("ya.make", "GO_LIBRARY()\nSRCS(answer.c)\nEND()\n")
        self.write("answer.c", "int answer(void) { return 42; }\n")
        graph = self.graph()
        compiled = self.node(graph, "answer.c.o")
        archive = self.node(graph, self.project_name + ".a")
        self.assertIn(compiled["uid"], archive["deps"])
        self.assertTrue(set(compiled["outputs"]) & set(archive["inputs"]))
