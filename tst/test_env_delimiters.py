import os
import unittest
from unittest.mock import patch

import lib
from test_test_macro_values import option_values

BODIES = {
    "separators": "ENV(CHAIN=RED=1;BLUE=2)\nENV(TRAIL=left;)\nENV(EMPTY=;;AFTER=tail;)\n",
    "quoted": "ENV(CHAIN=\"RED=1; BLUE=2; GREEN=3\")\nENV(PAIR='A=1;B=2')\nENV(ESC=one\\;TWO=two)\n",
    "phases": "SET(PARTS FIRST=one;SECOND=two;)\nENV($PARTS)\nSET(PARTS LAST=changed;)\nENV(AFTER=yes)\n",
    "clear": "ENV(BEFORE=gone;DROP=gone)\nSET(TEST_ENV_VALUE)\nENV(AFTER=kept;TAIL=kept;)\n",
    "inherit": "ENV(LEFT=yes;INHERITED;T196_NOT_PRESENT;)\n",
    "ordinary": "ENV(SINGLE='one')\nENV(SPACES=\"left right\")\nENV(EMPTY='')\n",
}

def fixture(mode):
    if mode in ("python2", "python3", "python2data"):
        from test_no_doctests import fixture as base
        files,target,flags=base("alias-PY2TEST" if mode.startswith("python2") else "ordinary")
        files[target+"/ya.make"]=files[target+"/ya.make"].replace("END()", BODIES["quoted"]+"NO_DOCTESTS()\nEND()")
        if mode=="python2data":
            files[target+"/ya.make"]=files[target+"/ya.make"].replace("END()", "SET(VALIDATE_DATA yes)\nDATA(sbr://123=grove/input)\nEND()")
        return files,target,flags
    return {"grove/check/ya.make":"EXECTEST()\nRUN(true)\n"+BODIES[mode]+"END()\n"}, "grove/check", []

EXPECTED = {
    "separators": ["CHAIN=RED=1", "BLUE=2", "TRAIL=left", "EMPTY=", "AFTER=tail"],
    "quoted": ["CHAIN=RED=1", "BLUE=2", "GREEN=3", "PAIR='A=1", "B=2'", "ESC=one", "TWO=two"],
    "phases": ["FIRST=one", "SECOND=two", "AFTER=yes"],
    "clear": ["AFTER=kept", "TAIL=kept"],
    "inherit": ["LEFT=yes", "INHERITED=from parent with spaces"],
    "ordinary": ["SINGLE='one'", "SPACES=left", "EMPTY=''"],
}


class TestEnvDelimitersTest(unittest.TestCase):
    def test_native_dart_delimiters_follow_shell_parsing_and_preserve_order(self):
        for mode, expected in EXPECTED.items():
            with self.subTest(mode=mode), patch.dict(os.environ, {"INHERITED": "from parent with spaces"}):
                files, target, flags = fixture(mode)
                graph = lib.make(files, target, "-t", "--sandboxing", *flags)
                run = lib.node_by_output(graph, "$(B)/grove/check/test-results/exectest/meta.json")
                self.assertEqual(option_values(run, "--env"), expected)

    def test_python_suites_share_the_same_native_metadata_delimiters(self):
        for mode in ("python2", "python3", "python2data"):
            with self.subTest(mode=mode):
                files, target, flags = fixture(mode)
                graph = lib.make(files, target, "-t", "--sandboxing", *flags)
                expected = EXPECTED["quoted"] + ["YA_PYTEST_DISABLE_DOCTEST=yes"]
                if mode.startswith("python2"):
                    expected.insert(0, "YA_PYTEST_DISABLE_DOCTEST=yes")
                suites = ("pytest", "import_test", "py2_flake8") if mode.startswith("python2") else ("py3test", "import_test", "flake8")
                if mode == "python2data":
                    suites += ("validate_data_sbr",)
                for suite in suites:
                    run = lib.node_by_output(graph, f"$(B)/{target}/test-results/{suite}/meta.json")
                    self.assertEqual(option_values(run, "--env"), expected)
                suffix = "yapyc" if mode.startswith("python2") else "yapyc3"
                lib.node_by_output(graph, f"$(B)/{target}/a.py.{suffix}")


if __name__ == "__main__":
    unittest.main(verbosity=2)
