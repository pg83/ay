from tst.make.lib import MakeTest


class TestResourceRequirementsTest(MakeTest):
    def test_memory_requirements_select_the_default_cpu_budget(self):
        self.write("test_answer.py", "def test_answer(): assert True\n")
        for ram, cpu in ((4, 2), (8, 3)):
            with self.subTest(ram=ram):
                self.write("ya.make", f"PY3TEST()\nTEST_SRCS(test_answer.py)\nREQUIREMENTS(ram:{ram})\nEND()\n")
                node = self.node(self.graph("-t"), "test-results/py3test/meta.json")
                self.assertEqual(float(node["requirements"]["ram"]), ram)
                self.assertEqual(float(node["requirements"]["cpu"]), cpu)

    def test_copydata_tag_selects_copying_test_data(self):
        self.write("ya.make", "PY3TEST()\nTEST_SRCS(test_answer.py)\nTAG(ya:copydata)\nEND()\n")
        self.write("test_answer.py", "def test_answer(): assert True\n")
        node = self.node(self.graph("-t"), "test-results/py3test/meta.json")
        args = [arg for command in node["cmds"] for arg in command["cmd_args"]]
        self.assertEqual(args[args.index("--data-to-environment") + 1], "copy")

    def test_huge_logs_tag_raises_the_test_output_limit(self):
        self.write("ya.make", "PY3TEST()\nTEST_SRCS(test_answer.py)\nTAG(ya:huge_logs)\nEND()\n")
        self.write("test_answer.py", "def test_answer(): assert True\n")
        node = self.node(self.graph("-t"), "test-results/py3test/meta.json")
        self.assertEqual(float(node["requirements"]["test_output_limit"]), 1024 * 1024 * 1024)
