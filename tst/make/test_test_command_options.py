import base64
import shlex

from tst.make.lib import MakeTest, PROJECT


class TestCommandOptionsTest(MakeTest):
    def test_conftest_loading_policies_reach_python_test_environment(self):
        self.write("test_answer.py", "def test_answer(): assert True\n")
        for macro, value in (("CONFTEST_LOAD_POLICY_LOCAL", "LOCAL"), ("CONFTEST_LOAD_POLICY_LEGACY_GLOBAL", "LEGACY_GLOBAL")):
            with self.subTest(macro=macro):
                self.write("ya.make", f"PY3TEST()\nTEST_SRCS(test_answer.py)\n{macro}()\nEND()\n")
                args = [arg for command in self.node(self.graph("-t"), "test-results/py3test/meta.json")["cmds"] for arg in command["cmd_args"]]
                self.assertIn("CONFTEST_LOAD_POLICY=" + value, args)

    def test_trailing_recipe_separators_do_not_add_arguments(self):
        self.write("ya.make", f"PY3TEST()\nTEST_SRCS(test_answer.py)\nDEPENDS({PROJECT}/recipe)\nUSE_RECIPE({PROJECT}/recipe/recipe ; &&)\nEND()\n")
        self.write("test_answer.py", "def test_answer(): assert True\n")
        self.write("recipe/ya.make", "PY3_PROGRAM(recipe)\nPY_SRCS(MAIN main.py)\nEND()\n")
        self.write("recipe/main.py", "pass\n")
        args = [arg for command in self.node(self.graph("-t"), "test-results/py3test/meta.json")["cmds"] for arg in command["cmd_args"]]
        recipes = base64.b64decode(args[args.index("--recipes") + 1]).decode()
        commands = [shlex.split(line) for line in recipes.splitlines() if line.strip()]
        self.assertIn([f"{PROJECT}/recipe/recipe"], commands)

    def test_exectest_quoted_arguments_follow_makefile_unescaping(self):
        self.write("ya.make", 'EXECTEST()\nRUN(python3 --value "two words" "" "with\\\"quote" "ends with slash\\\\")\nEND()\n')
        args = [arg for command in self.node(self.graph("-t"), "test-results/exectest/meta.json")["cmds"] for arg in command["cmd_args"]]
        commands = [base64.b64decode(arg.split("commands=", 1)[1]).decode() for arg in args if "commands=" in arg]
        self.assertEqual(len(commands), 1)
        self.assertEqual(shlex.split(commands[0]), ["python3", "--value", "two words", "", 'with"quote', "ends with slash"])
