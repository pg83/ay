import os
from unittest.mock import patch

from tst.make.lib import MakeTest


class InheritedTestEnvironmentTest(MakeTest):
    def test_bare_environment_entries_inherit_only_existing_values(self):
        self.write("ya.make", "PY3TEST()\nTEST_SRCS(test_answer.py)\nENV(AY_E2E_INHERITED)\nENV(AY_E2E_UNDEFINED)\nEND()\n")
        self.write("test_answer.py", "def test_answer(): assert True\n")
        with patch.dict(os.environ, {"AY_E2E_INHERITED": "42"}):
            os.environ.pop("AY_E2E_UNDEFINED", None)
            node = self.node(self.graph("-t"), "test-results/py3test/meta.json")
        args = [arg for command in node["cmds"] for arg in command["cmd_args"]]
        self.assertIn("AY_E2E_INHERITED=42", args)
        self.assertFalse(any(arg.startswith("AY_E2E_UNDEFINED") for arg in args))
