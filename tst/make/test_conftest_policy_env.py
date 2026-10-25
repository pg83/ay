import json
import unittest

from tst import lib
from tst.make.test_python_suites import fixture as python_fixture
from tst.make.test_test_macro_values import option_values


MODULE = "grove/check"
BODIES = {
    "ordinary": "ENV(A=before)\n",
    "local": "CONFTEST_LOAD_POLICY_LOCAL()\n",
    "legacy": "CONFTEST_LOAD_POLICY_LEGACY_GLOBAL()\n",
    "both": "CONFTEST_LOAD_POLICY_LOCAL()\nCONFTEST_LOAD_POLICY_LEGACY_GLOBAL()\n",
    "reverse": "CONFTEST_LOAD_POLICY_LEGACY_GLOBAL()\nCONFTEST_LOAD_POLICY_LOCAL()\n",
    "mixed": ('ENV(A=before)\nSET_APPEND(TEST_ENV_VALUE "RAW=first value")\n'
              'CONFTEST_LOAD_POLICY_LOCAL()\nENV(B=after)\nCONFTEST_LOAD_POLICY_LOCAL()\n'),
    "overwrite": ('ENV(A=before)\nCONFTEST_LOAD_POLICY_LOCAL()\n'
                  'SET(TEST_ENV_VALUE "RESET=yes")\nCONFTEST_LOAD_POLICY_LEGACY_GLOBAL()\n'),
    "spaces": 'ENV(A=one two)\nSET_APPEND(TEST_ENV_VALUE "RAW=first value")\nENV("LAST=tail value")\n',
    "late": ('SET(VALUE first)\nENV(LABEL=$VALUE)\nSET_APPEND(TEST_ENV_VALUE "RAW=$VALUE")\n'
             'SET(VALUE last)\nCONFTEST_LOAD_POLICY_LOCAL()\n'),
    "set-late": ('SET(VALUE first)\nSET(TEST_ENV_VALUE "RAW=${VALUE}")\n'
                 'SET(VALUE last)\nCONFTEST_LOAD_POLICY_LOCAL()\n'),
    "clear": 'CONFTEST_LOAD_POLICY_LOCAL()\nSET(TEST_ENV_VALUE)\nENV(FINAL=yes)\n',
}
EXPECTED = {
    "ordinary": ["A=before"],
    "local": ["CONFTEST_LOAD_POLICY=LOCAL"],
    "legacy": ["CONFTEST_LOAD_POLICY=LEGACY_GLOBAL"],
    "both": ["CONFTEST_LOAD_POLICY=LOCAL", "CONFTEST_LOAD_POLICY=LEGACY_GLOBAL"],
    "reverse": ["CONFTEST_LOAD_POLICY=LEGACY_GLOBAL", "CONFTEST_LOAD_POLICY=LOCAL"],
    "mixed": ["A=before", "RAW=first", "CONFTEST_LOAD_POLICY=LOCAL", "B=after", "CONFTEST_LOAD_POLICY=LOCAL"],
    "overwrite": ["RESET=yes", "CONFTEST_LOAD_POLICY=LEGACY_GLOBAL"],
    "spaces": ["A=one two", "RAW=first", "LAST=tail value"],
    "late": ["LABEL=first", "RAW=last", "CONFTEST_LOAD_POLICY=LOCAL"],
    "set-late": ["RAW=first", "CONFTEST_LOAD_POLICY=LOCAL"],
    "clear": ["FINAL=yes"],
}


def fixture(mode):
    if mode == "existing-whole" or mode.startswith("python-"):
        body = ('NO_PYTHON_INCLUDES()\nSTYLE_RUFF()\n'
                'ENV(TEST_PROGRAMS=programs/first: programs/second: programs/third)\n'
                'ENV("MESSAGE=${LABEL} ending")\nENV(EMPTY=)\nENV(INHERITED)\n'
                'SET(LABEL first second)\n')
        macro = "PY3TEST" if mode == "existing-whole" else mode.removeprefix("python-")
        if mode != "existing-whole":
            body += BODIES["mixed"]
        files = python_fixture(body, macro=macro)
        files["library/python/pytest/ya.make"] = f"LIBRARY()\n{lib.BARE_MODULE}END()\n"
        files.update({"lint/flake8.conf": "", "lint/ruff.toml": ""})
        if macro != "PY3TEST":
            from tst.make.test_python2 import python2_base
            files.update(python2_base())
            files["build/config/tests/py_style/default_configs.json"] = json.dumps({
                "py2_flake8": "lint/flake8.conf", "ruff": "lint/ruff.toml",
            })
            files["build/external_resources/flake8_py2/ya.make"] = (
                "RESOURCES_LIBRARY()\nDECLARE_EXTERNAL_RESOURCE(FLAKE8_PY2 sbr:123)\nEND()\n"
            )
        return files
    if mode.startswith("existing-shared-"):
        project = mode.removeprefix("existing-shared-")
        return {
            "common/values.inc": "ENV(CONFIG=$CONFIG trailing words)\nUSE_RECIPE(recipe/run $CONFIG)\n",
            project + "/ya.make": ("EXECTEST()\nRUN(echo)\nSET(CONFIG initial)\n"
                                  "INCLUDE(${ARCADIA_ROOT}/common/values.inc)\n"
                                  "SET(CONFIG " + project + "/final value)\nEND()\n"),
        }
    return {MODULE + "/ya.make": "EXECTEST()\nRUN(true)\n" + BODIES[mode] + "END()\n"}


class ConftestPolicyEnvTest(unittest.TestCase):
    def test_policy_macros_share_the_ordered_test_environment_variable(self):
        for mode, expected in EXPECTED.items():
            with self.subTest(mode=mode):
                graph = lib.make(fixture(mode), MODULE, "-t")
                run = lib.node_by_output(graph, f"$(B)/{MODULE}/test-results/exectest/meta.json")
                self.assertEqual(option_values(run, "--env"), expected)
                self.assertNotIn("CONFTEST_LOAD_POLICY", run["env"])

    def test_cli_default_and_common_process_environment_remain_separate(self):
        graph = lib.make(fixture("mixed"), MODULE, "-t", "-DTEST_ENV_VALUE=CLI=first",
                         env={"YA_T148_ENV": "process value"})
        run = lib.node_by_output(graph, f"$(B)/{MODULE}/test-results/exectest/meta.json")
        self.assertEqual(option_values(run, "--env"), ["CLI=first", *EXPECTED["mixed"]])
        self.assertEqual(run["env"]["YA_T148_ENV"], "process value")
        self.assertNotIn("CLI", run["env"])

    def test_python_aliases_propagate_environment_to_real_import_and_style_suites(self):
        for macro, suites in (("PY3TEST", ("py3test", "import_test", "flake8", "ruff")),
                              ("PY2TEST", ("pytest", "import_test", "py2_flake8", "ruff"))):
            with self.subTest(macro=macro):
                graph = lib.make(fixture("python-" + macro), "lib", "-t",
                                 env={"INHERITED": "from parent with spaces"})
                expected = ["TEST_PROGRAMS=programs/first: programs/second: programs/third",
                            "MESSAGE= ending", "EMPTY=", "INHERITED=from parent with spaces", *EXPECTED["mixed"]]
                for suite in suites:
                    run = lib.node_by_output(graph, f"$(B)/lib/test-results/{suite}/meta.json")
                    self.assertEqual(option_values(run, "--env"), expected)


if __name__ == "__main__":
    unittest.main(verbosity=2)
