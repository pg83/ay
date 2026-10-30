import os
from unittest.mock import patch

from tst.make.lib import MakeTest


class GraphEnvironmentEncodingTest(MakeTest):
    def inherited_argument(self, value):
        self.write("ya.make", "PY3TEST()\nTEST_SRCS(test_answer.py)\nENV(AY_E2E_ENCODING)\nEND()\n")
        self.write("test_answer.py", "def test_answer(): assert True\n")
        with patch.dict(os.environ, {"AY_E2E_ENCODING": value}):
            node = self.node(self.graph("-t"), "test-results/py3test/meta.json")
        args = [arg for command in node["cmds"] for arg in command["cmd_args"]]
        return next(arg for arg in args if arg.startswith("AY_E2E_ENCODING="))

    def test_control_characters_survive_graph_serialization(self):
        value = "before\r\x01after"
        self.assertEqual(self.inherited_argument(value), "AY_E2E_ENCODING=" + value)

    def test_invalid_utf8_environment_bytes_are_rejected(self):
        self.write("ya.make", "PY3TEST()\nTEST_SRCS(test_answer.py)\nENV(AY_E2E_ENCODING)\nEND()\n")
        self.write("test_answer.py", "def test_answer(): assert True\n")
        with patch.dict(os.environ, {"AY_E2E_ENCODING": "before\udcffafter\udcfe"}):
            result = self.make("-t")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("utf-8", result.stderr)
