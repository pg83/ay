import unittest

import lib
from test_python_suites import fixture as python_fixture
from test_test_macro_values import option_values


BODIES = {
    "unknown": ('ENV(BARE=$MISSING/end)\nENV(BRACED=${MISSING}/end)\n'
                'ENV(FUTURE=$LATER/end)\nSET(LATER final)\nENV(EMPTY=)\n'),
    "phases": ('SET(VALUE first)\nENV(BOUND=$VALUE)\nENV(BRACED=${VALUE})\n'
               'SET_APPEND(TEST_ENV_VALUE RAW_BARE=$VALUE)\n'
               'SET_APPEND(TEST_ENV_VALUE RAW_BRACED=${VALUE})\n'
               'SET(VALUE last)\nSET_APPEND(TEST_ENV_VALUE UNBOUND_RAW=$RAW_MISSING/end)\n'
               'SET_APPEND(TEST_ENV_VALUE CLEARED_RAW=${RAW_MISSING}/end)\n'),
    "escaped": (r'ENV(ESCAPED_BARE=\$MISSING/end)' + '\n'
                + r'ENV(ESCAPED_BRACED=\${MISSING}/end)' + '\n'
                + 'SET(VALUE first)\n' + r'ENV(ESCAPED_BOUND=\${VALUE})' + '\n'
                + r'SET_APPEND(TEST_ENV_VALUE ESCAPED_RAW=\${VALUE})' + '\nSET(VALUE last)\n'),
}


def fixture(mode):
    if mode in BODIES:
        return {"grove/check/ya.make": 'EXECTEST()\nRUN(true)\n' + BODIES[mode] + 'END()\n'}, "grove/check"
    body = ('NO_PYTHON_INCLUDES()\nPEERDIR(runtimes/maple)\n'
            'ENV(ENGINE=$MAPLE_ENGINE_RESOURCE_GLOBAL/bin/engine)\n'
            'ENV(UNBOUND=$ABSENT_ENGINE/bin/engine)\n')
    if mode == "peer-braced":
        body = body.replace('$MAPLE_ENGINE_RESOURCE_GLOBAL', '${MAPLE_ENGINE_RESOURCE_GLOBAL}')
    files = python_fixture(body, macro="PY3TEST")
    files.update({
        "library/python/pytest/ya.make": f'LIBRARY()\n{lib.BARE_MODULE}END()\n',
        "runtimes/maple/ya.make": 'RESOURCES_LIBRARY()\nDECLARE_EXTERNAL_RESOURCE(MAPLE_ENGINE sbr:741)\nEND()\n',
        "lint/flake8.conf": "",
    })
    return files, "lib"


class TestEnvDeferredRefsTest(unittest.TestCase):
    def test_unknown_and_call_time_source_references_follow_native_expansion_phases(self):
        expected = {
            "unknown": ["BARE=$MISSING/end", "BRACED=/end", "FUTURE=final/end", "EMPTY="],
            "phases": ["BOUND=first", "BRACED=first", "RAW_BARE=last", "RAW_BRACED=first",
                       "UNBOUND_RAW=$RAW_MISSING/end", "CLEARED_RAW=/end"],
            "escaped": ["ESCAPED_BARE=$MISSING/end", "ESCAPED_BRACED=/end",
                        "ESCAPED_BOUND=first", "ESCAPED_RAW=last"],
        }
        for mode, values in expected.items():
            with self.subTest(mode=mode):
                files, target = fixture(mode)
                graph = lib.make(files, target, "-t", "--cache-tests")
                run = lib.node_by_output(graph, f"$(B)/{target}/test-results/exectest/meta.json")
                self.assertEqual(option_values(run, "--env"), values)

    def test_real_suites_resolve_resource_peers_while_style_retains_unbound_references(self):
        for mode in ("peer", "peer-braced"):
            with self.subTest(mode=mode):
                files, target = fixture(mode)
                graph = lib.make(files, target, "-t", "--cache-tests")
                resource = lib.node_by_output(graph, "$(B)/resources/MAPLE_ENGINE")
                self.assertIn("sbr:741", resource["cmds"][0]["cmd_args"])
                for suite in ("py3test", "import_test", "flake8"):
                    run = lib.node_by_output(graph, f"$(B)/lib/test-results/{suite}/meta.json")
                    engine = "$MAPLE_ENGINE_RESOURCE_GLOBAL" if suite == "flake8" else "$(B)/resources/MAPLE_ENGINE"
                    if mode == "peer-braced":
                        engine = "${MAPLE_ENGINE_RESOURCE_GLOBAL}"
                    self.assertEqual(option_values(run, "--env"), [
                        f"ENGINE={engine}/bin/engine", "UNBOUND=$ABSENT_ENGINE/bin/engine",
                    ])
                    self.assertNotIn("ENGINE", run["env"])
                    if suite != "flake8":
                        self.assertIn("MAPLE_ENGINE_RESOURCE_GLOBAL::$(B)/resources/MAPLE_ENGINE",
                                      option_values(run, "--global-resource"))


if __name__ == "__main__":
    unittest.main(verbosity=2)
