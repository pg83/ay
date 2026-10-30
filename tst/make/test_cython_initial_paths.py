from tst.make.lib import MakeTest, PROJECT


class CythonInitialPathsTest(MakeTest):
    def check_initial_path(self, source):
        self.write("ya.make", f"PY3_LIBRARY()\nPY_SRCS({source}=e2e.answer)\nEND()\n")
        self.write("answer.pyx", "def answer():\n    return 42\n")
        graph = self.graph()
        generated = [node for node in graph["graph"] if f"$(S)/{PROJECT}/answer.pyx" in node["inputs"] and any(path.endswith(".cpp") for path in node["outputs"])]
        self.assertEqual(len(generated), 1)
        args = [arg for command in generated[0]["cmds"] for arg in command["cmd_args"]]
        self.assertIn(f"set_initial_path={PROJECT}/answer.pyx", args)

    def test_rooted_sources_keep_their_source_relative_initial_path(self):
        self.check_initial_path(f"${{ARCADIA_ROOT}}/{PROJECT}/answer.pyx")

    def test_source_root_relative_sources_keep_their_initial_path(self):
        self.check_initial_path(f"{PROJECT}/answer.pyx")
