from tst.make.lib import MakeTest, PROJECT


class WindowsCythonTest(MakeTest):
    def test_cython_receives_the_windows_system_name(self):
        self.write("ya.make", "PY3_LIBRARY()\nPY_SRCS(answer.pyx)\nEND()\n")
        self.write("answer.pyx", "def answer():\n    return 42\n")
        graph = self.graph("--target-platform=default-win-x86_64")
        generated = [node for node in graph["graph"] if f"$(S)/{PROJECT}/answer.pyx" in node["inputs"] and any(path.endswith(".cpp") for path in node["outputs"])]
        self.assertEqual(len(generated), 1)
        args = [arg for command in generated[0]["cmds"] for arg in command["cmd_args"]]
        self.assertIn("UNAME_SYSNAME=Windows", args)
