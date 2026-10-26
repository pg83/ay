import json

from tst.make.lib import MakeTest, PROJECT


class TestDataTest(MakeTest):
    def test_external_sandbox_data(self):
        self.write("ya.make", "PY3TEST()\nTEST_SRCS(test_answer.py)\nDATA(ext:graph/answer.bin)\nEND()\n")
        self.write("test_answer.py", "def test_answer():\n    assert True\n")
        self.write("graph/answer.bin.external", json.dumps({"storage": "SANDBOX", "resource_id": 123456789, "file_name": "answer.bin", "download_link": "https://proxy.sandbox.yandex-team.ru/123456789", "resource_link": "https://sandbox.yandex-team.ru/resource/123456789/view", "md5": "0" * 32, "skynet_id": "rbtorrent:" + "0" * 40}))
        graph = self.graph("-t")
        commands = [command["cmd_args"] for node in graph["graph"] for command in node["cmds"]]
        self.assertTrue(any("--sandbox-resource" in args and "123456789=graph" in args for args in commands), repr([arg for args in commands for arg in args if "123456789" in arg or "sandbox" in arg]))

    def test_sandbox_data(self):
        self.write("ya.make", "PY3TEST()\nTEST_SRCS(test_answer.py)\nDATA(sbr://123456789=answer)\nEND()\n")
        self.write("test_answer.py", "def test_answer():\n    assert 42 == 42\n")
        graph = self.graph("-t")
        self.assertTrue(any("123456789" in json.dumps(node.get("cmds", [])) and "download" in json.dumps(node.get("cmds", [])) for node in graph["graph"]))

    def test_resource_validation(self):
        self.write("ya.make", "LIBRARY()\nFROM_SANDBOX(FILE 123456789 OUT_NOAUTO answer.txt)\nRESOURCE(answer.txt /answer)\nEND()\n")
        graph = self.graph("-t")
        self.assertTrue(any("validate_resource" in json.dumps(node.get("cmds", [])) for node in graph["graph"]))

    def test_test_recipe(self):
        self.write("ya.make", f"PY3TEST()\nTEST_SRCS(test_answer.py)\nDEPENDS({PROJECT}/recipe)\nUSE_RECIPE({PROJECT}/recipe/recipe --value 42)\nDATA_FILES({PROJECT}/data.txt)\nFORK_TESTS()\nTAG(e2e)\nREQUIREMENTS(cpu:1 ram:2)\nEND()\n")
        self.write("test_answer.py", "def test_answer():\n    assert 42 == 42\n")
        self.write("data.txt", "42\n")
        self.write("recipe/ya.make", "PY3_PROGRAM(recipe)\nPY_SRCS(MAIN main.py)\nEND()\n")
        self.write("recipe/main.py", 'import sys\nif sys.argv[-1] not in ("start", "stop"):\n    raise SystemExit(1)\n')
        graph = self.graph("-t", "--test-tag=e2e", "--test-tag=-ya:manual")
        self.node(graph, "recipe/recipe")
