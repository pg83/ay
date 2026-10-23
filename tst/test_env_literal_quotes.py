import unittest

import lib
from test_test_macro_values import option_values

BODIES = {
    "quotes": """ENV(SINGLE='1')
ENV(DOUBLE="2")
ENV("OUTER='3'")
ENV('OUTER_SINGLE="4"')
ENV(MIDDLE=pre'inner'post)
""",
    "phases": """SET(VALUE first)
ENV(BOUND='$VALUE')
ENV(BRACED='${VALUE}')
ENV(FUTURE='$LATER')
SET(LATER last)
ENV(EMPTY='')
ENV(AFTER=last)
""",
    "escaped": (r"ENV(BARE=\$MISSING/end)" + "\n"
                + r"ENV(BRACED=\${MISSING}/end)" + "\n"
                + r"ENV(QUOTED=\"word\")" + "\n"),
    "clear": """ENV(BEFORE='first')
SET(TEST_ENV_VALUE)
ENV(AFTER='last')
""",
}


def fixture(mode):
    if mode == "java":
        from test_java_tests import junit_tree
        return junit_tree('JUNIT5()\n' + BODIES["quotes"] + 'END()\n'), "proj/sub/ut", []
    if mode == "python":
        from test_no_doctests import fixture as python_fixture
        files, target, flags = python_fixture("ordinary")
        files[target + "/ya.make"] = files[target + "/ya.make"].replace("END()", BODIES["quotes"] + "NO_DOCTESTS()\nEND()")
        return files, target, flags
    return {"grove/check/ya.make": 'EXECTEST()\nRUN(true)\n' + BODIES[mode] + 'END()\n'}, "grove/check", []


EXPECTED = {
    "quotes": ["SINGLE='1'", "DOUBLE=2", "OUTER='3'", "OUTER_SINGLE=4", "MIDDLE=pre'inner'post"],
    "phases": ["BOUND='first'", "BRACED='first'", "FUTURE='last'", "EMPTY=''", "AFTER=last"],
    "escaped": ["BARE=$MISSING/end", "BRACED=/end", "QUOTED=word"],
    "clear": ["AFTER='last'"],
}


class EnvLiteralQuotesTest(unittest.TestCase):
    def test_literal_quotes_follow_native_environment_quoting(self):
        for mode, expected in EXPECTED.items():
            with self.subTest(mode=mode):
                files, target, flags = fixture(mode)
                graph = lib.make(files, target, "-t", "--sandboxing", *flags)
                run = lib.node_by_output(graph, f"$(B)/{target}/test-results/exectest/meta.json")
                self.assertEqual(option_values(run, "--env"), expected)


    def test_python_real_import_and_style_suites_keep_literal_quotes(self):
        files, target, flags = fixture("python")
        graph = lib.make(files, target, "-t", "--sandboxing", *flags)
        for suite in ("py3test", "import_test", "flake8"):
            run = lib.node_by_output(graph, f"$(B)/{target}/test-results/{suite}/meta.json")
            self.assertEqual(option_values(run, "--env"), EXPECTED["quotes"] + ["YA_PYTEST_DISABLE_DOCTEST=yes"])
        lib.node_by_output(graph, f"$(B)/{target}/a.py.yapyc3")

    def test_java_test_environment_uses_the_same_lexical_literals(self):
        files, target, flags = fixture("java")
        graph = lib.make(files, target, "-t", "--sandboxing", *flags)
        runs = [node for node in graph["graph"] if node.get("kv", {}).get("run_test_node")]
        self.assertTrue(runs)
        for run in runs:
            self.assertEqual(option_values(run, "--env"), EXPECTED["quotes"])


if __name__ == '__main__':
    unittest.main()
