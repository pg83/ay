from tst.make.lib import MakeTest


class TestConfigurationFlagsTest(MakeTest):
    def test_internal_configuration_flags_are_omitted_from_the_test_runner(self):
        self.write("ya.make", "PY3TEST()\nTEST_SRCS(test_answer.py)\nEND()\n")
        self.write("test_answer.py", "def test_answer(): assert True\n")
        node = self.node(self.graph("-t", "-DCONSISTENT_DEBUG=yes", "-DE2E_ANSWER=42"), "test-results/py3test/meta.json")
        args = [arg for command in node["cmds"] for arg in command["cmd_args"]]
        flags = [args[index + 1] for index, arg in enumerate(args) if arg == "--flags"]
        self.assertIn("E2E_ANSWER=42", flags)
        self.assertFalse(any(flag.startswith("CONSISTENT_DEBUG=") for flag in flags))
