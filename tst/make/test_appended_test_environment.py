from tst.make.lib import MakeTest


class AppendedTestEnvironmentTest(MakeTest):
    def test_appended_test_environment_values_expand_module_variables(self):
        self.write("ya.make", "PY3TEST()\nSET(E2E_VALUE 42)\nSET_APPEND(TEST_ENV_VALUE E2E_ANSWER=${E2E_VALUE})\nTEST_SRCS(test_answer.py)\nEND()\n")
        self.write("test_answer.py", "def test_answer(): assert True\n")
        node = self.node(self.graph("-t"), "test-results/py3test/meta.json")
        args = [arg for command in node["cmds"] for arg in command["cmd_args"]]
        self.assertIn("E2E_ANSWER=42", args)
