from tst.make.lib import MakeTest


class PythonOwnershipGraphTest(MakeTest):
    def test_python_program_generation_passes_ownership_checks(self):
        self.write("ya.make", "PY3_PROGRAM(answer)\nPY_SRCS(MAIN main.py)\nEND()\n")
        self.write("main.py", "print(42)\n")
        self.node(self.graph(env={"AY_DEBUG_OWNERSHIP": "1"}), "answer")
