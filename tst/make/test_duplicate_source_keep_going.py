from tst.make.lib import MakeTest, PROJECT


class DuplicateSourceKeepGoingTest(MakeTest):
    def test_keep_going_retains_one_object_for_each_repeated_source(self):
        self.write("ya.make", "LIBRARY(answer)\nSRC(answer.S)\nSRC(./answer.S)\nSRC(answer.cpp)\nSRC(./answer.cpp)\nEND()\n")
        self.write("answer.S", ".text\n.globl assembly_answer\nassembly_answer:\n    ret\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        graph = self.graph("-k")
        archive = self.node(graph, "libanswer.a")
        for source in ("answer.S", "answer.cpp"):
            with self.subTest(source=source):
                compiled = self.node(graph, source + ".o")
                self.assertIn(f"$(S)/{PROJECT}/{source}", compiled["inputs"])
                self.assertIn(compiled["uid"], archive["deps"])
                self.assertIn(f"$(B)/{PROJECT}/{source}.o", archive["inputs"])
