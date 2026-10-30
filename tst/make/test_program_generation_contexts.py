from tst.make.lib import MakeTest, PROJECT


class ProgramGenerationContextsTest(MakeTest):
    def generator(self):
        self.write("tool/ya.make", "PY3_PROGRAM(generator)\nPY_SRCS(MAIN main.py)\nEND()\n")
        self.write("tool/main.py", "print(42)\n")

    def test_generated_go_outputs_are_collected_without_explicit_sources(self):
        self.generator()
        self.write("ya.make", f"GO_LIBRARY()\nRUN_PROGRAM({PROJECT}/tool OUT_NOAUTO generated.go)\nEND()\n")
        graph = self.graph()
        generated = self.node(graph, "generated.go")
        library = self.node(graph, f"{PROJECT}.a")
        self.assertIn(generated["uid"], library["deps"])
        self.assertIn(f"$(B)/{PROJECT}/generated.go", library["inputs"])

    def test_generated_object_outputs_are_added_to_the_archive(self):
        self.generator()
        self.write("ya.make", f"LIBRARY()\nRUN_PROGRAM({PROJECT}/tool OUT_NOAUTO generated.o)\nEND()\n")
        graph = self.graph()
        generated = self.node(graph, "generated.o")
        library = self.node(graph, f"lib{PROJECT}.a")
        self.assertIn(generated["uid"], library["deps"])
        self.assertIn(f"$(B)/{PROJECT}/generated.o", library["inputs"])

    def test_environment_names_without_values_are_empty(self):
        self.generator()
        self.write("ya.make", f"LIBRARY()\nRUN_PROGRAM({PROJECT}/tool ENV E2E_EMPTY OUT generated.cpp)\nEND()\n")
        generated = self.node(self.graph(), "generated.cpp")
        self.assertEqual(generated["cmds"][0]["env"]["E2E_EMPTY"], "")
