import base64
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

    def test_skipped_suite_has_no_run_node(self):
        self.write("ya.make", f'PY3TEST()\nSKIP_TEST("not in production")\nTEST_SRCS(test_answer.py)\nDATA(sbr://123456789=answer)\nDEPENDS({PROJECT}/tool)\nEND()\n')
        self.write("test_answer.py", "def test_answer():\n    assert 42 == 42\n")
        self.write("tool/ya.make", "PROGRAM(tool)\nSRCS(main.cpp)\nEND()\n")
        self.write("tool/main.cpp", "int main() { return 0; }\n")
        graph = self.graph("-t", env={"YA_STRIP_IDLE_BUILD_RESULTS": "0"})
        commands = [json.dumps(node.get("cmds", [])) for node in graph["graph"]]
        self.assertFalse([command for command in commands if "123456789" in command and "download" in command])
        self.assertFalse([command for command in commands if "run_test" in command and "py3test" in command])
        self.assertTrue([command for command in commands if "import_test" in command])
        self.node(graph, "tool/tool")

    def test_skipped_native_suites(self):
        self.write("answer.cpp", '#include <library/cpp/testing/unittest/registar.h>\nY_UNIT_TEST_SUITE(Answer) { Y_UNIT_TEST(Value) { UNIT_ASSERT_EQUAL(42, 42); } }\n')
        self.write("answer_test.go", 'package answer\nimport "testing"\nfunc TestAnswer(t *testing.T) {}\n')
        for module, suite, kept in (("UNITTEST()\nSRCS(answer.cpp)", "unittest", PROJECT), ("GO_TEST()\nGO_TEST_SRCS(answer_test.go)", PROJECT, "test-results/gofmt/meta.json")):
            with self.subTest(module=module):
                self.write("ya.make", f"{module}\nSKIP_TEST(TODO)\nEND()\n")
                graph = self.graph("-t", env={"YA_STRIP_IDLE_BUILD_RESULTS": "0"})
                outputs = [path for node in graph["graph"] for path in node["outputs"]]
                self.assertNotIn(f"$(B)/{PROJECT}/test-results/{suite}/meta.json", outputs)
                self.node(graph, kept)

    def test_resource_validation(self):
        self.write("ya.make", "LIBRARY()\nFROM_SANDBOX(FILE 123456789 OUT_NOAUTO answer.txt)\nRESOURCE(answer.txt /answer)\nEND()\n")
        graph = self.graph("-t")
        self.assertTrue(any("validate_resource" in json.dumps(node.get("cmds", [])) for node in graph["graph"]))

    def test_test_recipe_module_name(self):
        self.write("ya.make", "RECURSE(unit)\n")
        self.write("recipe/ya.make", "PY3_PROGRAM(recipe)\nPY_SRCS(MAIN main.py)\nEND()\n")
        self.write("recipe/main.py", "pass\n")
        self.write("unit/test.cpp", "#include <library/cpp/testing/gtest/gtest.h>\nTEST(Answer, Value) { EXPECT_EQ(42, 42); }\n")
        self.write("unit/test_answer.py", "def test_answer():\n    assert 42 == 42\n")
        recipe = f"DEPENDS({PROJECT}/recipe)\nUSE_RECIPE({PROJECT}/recipe/recipe ${{MODDIR}}/${{REALPRJNAME}})\n"
        for module, sources, name in (("GTEST(answer)", "SRCS(test.cpp)", "answer"), ("GTEST()", "SRCS(test.cpp)", f"{PROJECT}-unit"), ("PY3TEST()", "TEST_SRCS(test_answer.py)", f"{PROJECT}-unit")):
            with self.subTest(module=module):
                self.write("unit/ya.make", f"{module}\n{recipe}{sources}\nEND()\n")
                graph = self.graph("-t")
                recipes = [base64.b64decode(args[args.index("--recipes") + 1]).decode() for node in graph["graph"] for args in (command["cmd_args"] for command in node["cmds"]) if "--recipes" in args]
                self.assertTrue(recipes)
                for value in recipes:
                    self.assertIn(f"{PROJECT}/recipe/recipe {PROJECT}/unit/{name}", value)

    def test_test_recipe(self):
        self.write("ya.make", f"PY3TEST()\nTEST_SRCS(test_answer.py)\nDEPENDS({PROJECT}/recipe)\nUSE_RECIPE({PROJECT}/recipe/recipe --value 42)\nDATA_FILES({PROJECT}/data.txt)\nFORK_TESTS()\nTAG(e2e)\nREQUIREMENTS(cpu:1 ram:2)\nEND()\n")
        self.write("test_answer.py", "def test_answer():\n    assert 42 == 42\n")
        self.write("data.txt", "42\n")
        self.write("recipe/ya.make", "PY3_PROGRAM(recipe)\nPY_SRCS(MAIN main.py)\nEND()\n")
        self.write("recipe/main.py", 'import sys\nif sys.argv[-1] not in ("start", "stop"):\n    raise SystemExit(1)\n')
        graph = self.graph("-t", "--test-tag=e2e", "--test-tag=-ya:manual")
        self.node(graph, "recipe/recipe")
