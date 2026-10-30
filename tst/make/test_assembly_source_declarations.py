from tst.make.lib import MakeTest, PROJECT


class AssemblySourceDeclarationsTest(MakeTest):
    def fixture(self, declaration):
        self.write("ya.make", f"LIBRARY()\n{declaration}\nEND()\n")
        self.write("answer.S", ".text\n.globl answer\nanswer:\n    ret\n")

    def test_explicit_assembly_sources_produce_objects(self):
        self.fixture("SRC(answer.S)")
        compiled = self.node(self.graph(), "answer.S.o")
        self.assertIn(f"$(S)/{PROJECT}/answer.S", compiled["inputs"])

    def test_repeated_assembly_declarations_are_rejected(self):
        self.fixture("SRC(answer.S)\nSRC(./answer.S)")
        result = self.make()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("answer.S.o", result.stderr)
        self.assertIn("already added", result.stderr)
