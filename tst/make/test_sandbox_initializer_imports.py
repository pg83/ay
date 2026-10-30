from tst.make.lib import MakeTest


class SandboxInitializerImportsTest(MakeTest):
    def test_explicit_sandbox_imports_accept_initializer_paths(self):
        self.write("ya.make", f"SANDBOX_PY3_TASK(answer)\nPEERDIR({self.project_name}/task)\nREGISTER_SANDBOX_IMPORT({self.project_name}/task/__init__.py)\nEND()\n")
        self.write("__init__.py", "\n")
        self.write("task/ya.make", "PY3_LIBRARY()\nPY_SRCS(__init__.py)\nEND()\n")
        self.write("task/__init__.py", "from sandbox import sdk2\nclass Answer(sdk2.Task):\n    def on_execute(self):\n        pass\n")
        graph = self.graph()
        key = f"SANDBOX_TASK_REGISTRY.{self.project_name}.task={self.project_name}.task"
        packed = [arg for node in graph["graph"] for command in node["cmds"] for arg in command["cmd_args"] if arg.startswith("SANDBOX_TASK_REGISTRY.")]
        self.assertIn(key, packed)
        self.assertFalse(any(".__init__" in arg for arg in packed))
