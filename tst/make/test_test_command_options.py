import base64
import shlex

from tst.make.lib import MakeTest, PROJECT


class TestCommandOptionsTest(MakeTest):
    def test_recursive_variables_in_test_environment_are_configuration_errors(self):
        config = self.root / "build/ymake.core.conf"
        self.write_tree("build/ymake.core.conf", config.read_text() + "\nE2E_GROWING = prefix$E2E_GROWING\n")
        self.write("ya.make", "PY3TEST()\nTEST_SRCS(test_answer.py)\nENV(E2E_VALUE=$E2E_GROWING)\nEND()\n")
        self.write("test_answer.py", "def test_answer(): assert True\n")
        self.assertNotEqual(self.make("-t").returncode, 0)

    def test_exectest_requires_a_run_command(self):
        self.write("ya.make", "EXECTEST()\nEND()\n")
        self.assertNotEqual(self.make("-t").returncode, 0)

    def test_exectest_accepts_an_explicit_module_name(self):
        self.write("ya.make", "EXECTEST(answer)\nRUN(python3 --version)\nEND()\n")
        self.node(self.graph("-t"), "answer.pkg.fake")

    def test_python_subtest_forks_split_the_run_nodes(self):
        self.write("ya.make", "PY3TEST()\nTEST_SRCS(test_answer.py)\nFORK_SUBTESTS()\nSPLIT_FACTOR(2)\nEND()\n")
        self.write("test_answer.py", "def test_first(): assert True\ndef test_second(): assert True\n")
        graph = self.graph("-t")
        for chunk in range(2):
            args = [arg for command in self.node(graph, f"test-results/py3test/chunk{chunk}/meta.json")["cmds"] for arg in command["cmd_args"]]
            self.assertIn("--split-by-tests", args)
            self.assertEqual(args[args.index("--modulo") + 1], "2")
            self.assertEqual(args[args.index("--modulo-index") + 1], str(chunk))

    def test_python_sanitizer_flag_reaches_the_runner(self):
        self.write("ya.make", "PY3TEST()\nTEST_SRCS(test_answer.py)\nEND()\n")
        self.write("test_answer.py", "def test_answer(): assert True\n")
        args = [arg for command in self.node(self.graph("-t", "-DSANITIZER_TYPE=address"), "test-results/py3test/meta.json")["cmds"] for arg in command["cmd_args"]]
        self.assertEqual(args[args.index("--sanitize") + 1], "address")

    def test_distributed_fuzz_runs_truncate_logs(self):
        self.write("ya.make", "FUZZ()\nSRCS(fuzz.cpp)\nEND()\n")
        self.write("fuzz.cpp", '#include <stddef.h>\n#include <stdint.h>\nextern "C" int LLVMFuzzerTestOneInput(const uint8_t*, size_t) { return 0; }\n')
        graph = self.graph("-t", "--dist")
        commands = [command["cmd_args"] for node in graph["graph"] for command in node["cmds"] if "run_fuzz" in command["cmd_args"]]
        self.assertTrue(commands)
        for args in commands:
            self.assertIn("--truncate-logs", args)

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
