import json
import unittest

import lib
from test_python_suites import fixture as python_fixture
from test_test_macro_values import option_values


PROJECT = "grove/specs"
BODIES = {
    "ordinary": "",
    "disabled": "NO_DOCTESTS()\n",
    "equivalent": 'ENV(YA_PYTEST_DISABLE_DOCTEST="yes")\n',
    "mixed": ('ENV(BEFORE=first)\nNO_DOCTESTS()\nNO_DOCTESTS()\n'
              'ENV(YA_PYTEST_DISABLE_DOCTEST=no)\nENV(AFTER=last)\n'),
    "clear": 'NO_DOCTESTS()\nSET(TEST_ENV_VALUE)\nENV(AFTER=last)\n',
    "raw-late": ('SET(VALUE first)\nNO_DOCTESTS()\n'
                 'SET_APPEND(TEST_ENV_VALUE "RAW=$VALUE")\nSET(VALUE last)\n'),
}
EXPECTED = {
    "ordinary": [],
    "disabled": ["YA_PYTEST_DISABLE_DOCTEST=yes"],
    "equivalent": ["YA_PYTEST_DISABLE_DOCTEST=yes"],
    "mixed": ["BEFORE=first", "YA_PYTEST_DISABLE_DOCTEST=yes", "YA_PYTEST_DISABLE_DOCTEST=yes",
              "YA_PYTEST_DISABLE_DOCTEST=no", "AFTER=last"],
    "clear": ["AFTER=last"],
    "raw-late": ["YA_PYTEST_DISABLE_DOCTEST=yes", "RAW=last"],
}


def fixture(mode):
    macro = "PY3TEST"
    if mode.startswith("alias-"):
        macro = mode.removeprefix("alias-")
        mode = "disabled"
    body = ('NO_PYTHON_INCLUDES()\nNO_IMPORT_TRACING()\nDISABLE(PYTHON_SQLITE3)\n'
            'ALLOCATOR(FAKE)\n' + BODIES[mode])
    files = python_fixture(body, macro=macro)
    files["library/python/pytest/ya.make"] = f"LIBRARY()\n{lib.BARE_MODULE}END()\n"
    files.update({"lint/flake8.conf": "", "lib/a.py": '""">>> 1 + 1\n2\n"""\n'})
    if macro in ("PY2TEST", "PYTEST_BIN"):
        from test_python2 import python2_base
        files.update(python2_base())
        files["build/config/tests/py_style/default_configs.json"] = '{"py2_flake8":"lint/flake8.conf"}'
        files["build/external_resources/flake8_py2/ya.make"] = lib.resource_lib("FLAKE8_PY2", 124)
    files = {(PROJECT + key[3:] if key.startswith("lib/") else key): value for key, value in files.items()}
    return files, PROJECT, []


class NoDoctestsTest(unittest.TestCase):
    def test_no_doctests_uses_the_ordered_test_environment_channel(self):
        for mode, expected in EXPECTED.items():
            with self.subTest(mode=mode):
                files, target, flags = fixture(mode)
                result = lib.make_process(files, target, "-t", *flags)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertNotIn('macro "NO_DOCTESTS"', result.stderr)
                graph = json.loads(result.stdout)
                for suite in ("py3test", "import_test", "flake8"):
                    run = lib.node_by_output(graph, f"$(B)/{PROJECT}/test-results/{suite}/meta.json")
                    self.assertEqual(option_values(run, "--env"), expected)
                    self.assertNotIn("YA_PYTEST_DISABLE_DOCTEST", run["env"])
                run = lib.node_by_output(graph, f"$(B)/{PROJECT}/test-results/py3test/meta.json")
                self.assertIn("--doctest-modules", run["cmds"][0]["cmd_args"])
                lib.node_by_output(graph, f"$(B)/{PROJECT}/a.py.yapyc3")

    def test_python_test_aliases_keep_the_same_environment_and_producers(self):
        for macro, suite, lint, bytecode in (("PY2TEST", "pytest", "py2_flake8", "yapyc"),):
            with self.subTest(macro=macro):
                files, target, flags = fixture("alias-" + macro)
                graph = lib.make(files, target, "-t", *flags)
                for name in (suite, "import_test", lint):
                    run = lib.node_by_output(graph, f"$(B)/{PROJECT}/test-results/{name}/meta.json")
                    self.assertEqual(option_values(run, "--env"), EXPECTED["disabled"])
                lib.node_by_output(graph, f"$(B)/{PROJECT}/a.py.{bytecode}")


if __name__ == "__main__":
    unittest.main(verbosity=2)
