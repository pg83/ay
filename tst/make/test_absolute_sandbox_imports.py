from tst.make.lib import MakeTest


class AbsoluteSandboxImportsTest(MakeTest):
    def test_missing_absolute_sandbox_imports_are_rejected(self):
        self.write("ya.make", f"SANDBOX_PY3_TASK(answer)\nPEERDIR({self.project_name}/task)\nREGISTER_SANDBOX_IMPORT(/{self.project_name}/task/__init__.py)\nEND()\n")
        self.write("__init__.py", "\n")
        self.write("task/ya.make", "PY3_LIBRARY()\nPY_SRCS(__init__.py)\nEND()\n")
        self.write("task/__init__.py", "from sandbox import sdk2\nclass Answer(sdk2.Task):\n    def on_execute(self):\n        pass\n")
        result = self.make()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("REGISTER_SANDBOX_IMPORT", result.stderr)
