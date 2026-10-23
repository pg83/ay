import base64
import unittest

import lib
from test_python_suites import fixture


def option_values(node, option):
    args = node["cmds"][-1]["cmd_args"]
    return [args[index + 1] for index, arg in enumerate(args) if arg == option]


class TestMacroValuesTest(unittest.TestCase):
    def test_python_and_lint_suites_preserve_whole_environment_values(self):
        files = fixture(
            'NO_PYTHON_INCLUDES()\nSTYLE_RUFF()\n'
            'ENV(TEST_PROGRAMS=programs/first: programs/second: programs/third)\n'
            'ENV("MESSAGE=${LABEL} ending")\nENV(EMPTY=)\nENV(INHERITED)\n'
            'SET(LABEL first second)\n', macro="PY3TEST",
        )
        files["library/python/pytest/ya.make"] = f"LIBRARY()\n{lib.BARE_MODULE}END()\n"
        graph = lib.make(files, "lib", "-t", env={"INHERITED": "from parent with spaces"})
        expected = [
            "TEST_PROGRAMS=programs/first: programs/second: programs/third",
            "MESSAGE= ending", "EMPTY=", "INHERITED=from parent with spaces",
        ]
        for name in ("py3test", "flake8", "ruff"):
            with self.subTest(suite=name):
                run = lib.node_by_output(graph, f"$(B)/lib/test-results/{name}/meta.json")
                self.assertEqual(option_values(run, "--env"), expected)
                self.assertNotIn("TEST_PROGRAMS", run["env"])

    def test_only_unresolved_bare_recipe_configuration_expands_after_the_include(self):
        files = {
            "suite/ya.make": (
                "EXECTEST()\nRUN(echo)\n"
                "INCLUDE(${ARCADIA_ROOT}/common/recipe.inc)\nSET(CONFIG suite/compose.yml)\n"
                "SET(EMPTY)\nEND()\n"
            ),
            "common/recipe.inc": (
                "USE_RECIPE(recipe/run --config $CONFIG --unknown $UNBOUND --empty $EMPTY)\n"
                "USE_RECIPE(recipe/check --config ${CONFIG})\n"
            ),
        }
        graph = lib.make(files, "suite", "-t")
        run = lib.node_by_output(graph, "$(B)/suite/test-results/exectest/meta.json")
        encoded, = option_values(run, "--recipes")
        self.assertEqual(base64.b64decode(encoded).decode(),
                         "recipe/run --config suite/compose.yml --unknown $UNBOUND --empty  \n "
                         "recipe/check --config \n")

    def test_shared_environment_and_defined_recipe_values_are_captured(self):
        files = {
            "common/values.inc": "ENV(CONFIG=$CONFIG trailing words)\nUSE_RECIPE(recipe/run $CONFIG)\n",
        }
        for project in ("one", "two"):
            files[f"{project}/ya.make"] = (
                "EXECTEST()\nRUN(echo)\nSET(CONFIG initial)\nINCLUDE(${ARCADIA_ROOT}/common/values.inc)\n"
                f"SET(CONFIG {project}/final value)\nEND()\n"
            )
        graph = lib.make(files, "one", "two", "-t")
        for project in ("one", "two"):
            with self.subTest(project=project):
                run = lib.node_by_output(graph, f"$(B)/{project}/test-results/exectest/meta.json")
                self.assertEqual(option_values(run, "--env"), ["CONFIG=initial trailing words"])
                encoded, = option_values(run, "--recipes")
                self.assertEqual(base64.b64decode(encoded), b"recipe/run initial \n")


if __name__ == "__main__":
    unittest.main(verbosity=2)
