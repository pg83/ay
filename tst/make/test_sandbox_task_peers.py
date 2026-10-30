from tst.make.lib import MakeTest, PROJECT


class SandboxTaskPeersTest(MakeTest):
    def task(self, declaration="SANDBOX_TASK(answer)", selectors=""):
        self.write("task/ya.make", f"{declaration}\n{selectors}\nPY_SRCS(__init__.py)\nEND()\n")
        self.write("task/__init__.py", "ANSWER = 42\n")

    def test_python_two_program_can_depend_on_a_python_two_task(self):
        self.task()
        self.write("ya.make", f"PY2_PROGRAM(consumer)\nPEERDIR({PROJECT}/task)\nPY_SRCS(MAIN answer.py)\nEND()\n")
        self.write("answer.py", "ANSWER = 42\n")
        inputs = {path for node in self.graph()["graph"] for path in node["inputs"]}
        self.assertIn(f"$(S)/{PROJECT}/task/__init__.py", inputs)

    def test_python_three_library_rejects_a_python_two_task(self):
        self.task()
        self.write("ya.make", f"PY3_LIBRARY()\nPEERDIR({PROJECT}/task)\nPY_SRCS(answer.py)\nEND()\n")
        self.write("answer.py", "ANSWER = 42\n")
        self.assertNotEqual(self.make().returncode, 0)

    def test_library_only_task_cannot_be_used_as_a_program(self):
        self.task("SANDBOX_PY3_TASK(answer)", "ONLY_TAGS(PY3)")
        self.write("ya.make", f"UNION()\nRUN_PROGRAM({PROJECT}/task OUT result.txt)\nEND()\n")
        self.assertNotEqual(self.make().returncode, 0)
