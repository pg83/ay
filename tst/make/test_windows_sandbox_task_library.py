from tst.make.lib import MakeTest


class WindowsSandboxTaskLibraryTest(MakeTest):
    def test_windows_python_three_task_libraries_use_the_python_three_prefix(self):
        self.write("ya.make", f"PY3_PROGRAM(answer)\nPY_SRCS(MAIN main.py)\nPEERDIR({self.project_name}/task)\nEND()\n")
        self.write("main.py", "print(42)\n")
        self.write("task/ya.make", "SANDBOX_PY23_3_TASK(task)\nPY_SRCS(answer.py)\nEND()\n")
        self.write("task/answer.py", "ANSWER = 42\n")
        graph = self.graph("--target-platform=default-win-x86_64")
        program = self.node(graph, "answer.exe")
        archives = {path for node in graph["graph"] for path in node["outputs"]
                    if path.startswith(f"$(B)/{self.project_name}/task/") and path.endswith(".lib")}
        self.assertTrue(archives)
        self.assertTrue(all(path.rsplit("/", 1)[-1].startswith("py3") for path in archives), archives)
        self.assertTrue(archives.intersection(program["inputs"]))

    def test_direct_windows_python_three_task_variants_use_the_python_three_prefix(self):
        self.write("ya.make", "SANDBOX_PY23_3_TASK(task)\nONLY_TAGS(PY3)\nPY_SRCS(answer.py)\nEND()\n")
        self.write("answer.py", "ANSWER = 42\n")
        graph = self.graph("--target-platform=default-win-x86_64")
        archives = {path for node in graph["graph"] for path in node["outputs"]
                    if path.startswith(f"$(B)/{self.project_name}/") and path.endswith(".lib")}
        self.assertTrue(archives)
        self.assertTrue(all(path.rsplit("/", 1)[-1].startswith("py3") for path in archives), archives)
