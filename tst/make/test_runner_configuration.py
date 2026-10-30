from tst.make.lib import MakeTest


class RunnerConfigurationTest(MakeTest):
    def pytest_project(self):
        self.write("ya.make", "PY3TEST()\nTEST_SRCS(test_answer.py)\nEND()\n")
        self.write("test_answer.py", "def test_answer(): assert True\n")

    def test_root_configuration_selects_test_output_style(self):
        self.pytest_project()
        self.write_tree("ya.conf", 'output_style = "jsonl"\n' + (self.root / "ya.conf").read_text())
        node = self.node(self.graph("-t", env={"YA_AUTO_DETECT_AGENT": "no"}), "test-results/py3test/meta.json")
        args = [arg for command in node["cmds"] for arg in command["cmd_args"]]
        self.assertEqual(args[args.index("--output-style") + 1], "jsonl")

    def test_mandatory_environment_names_are_preserved(self):
        self.pytest_project()
        node = self.node(self.graph("-t", env={
            "YA_MANDATORY_ENV_VARS": "YA_E2E_REQUIRED::YA_E2E_REQUIRED",
            "YA_E2E_REQUIRED": "42",
        }), "test-results/py3test/meta.json")
        self.assertEqual(node["env"]["YA_E2E_REQUIRED"], "42")
        names = node["env"]["YA_MANDATORY_ENV_VARS"].split(":")
        self.assertIn("YA_E2E_REQUIRED", names)
        self.assertEqual(names.count("YA_E2E_REQUIRED"), 1)
        self.assertNotIn("", names)
