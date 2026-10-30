import os
from unittest.mock import patch

from tst.make.lib import MakeTest, PROJECT


class EnvironmentTest(MakeTest):
    def test_rooted_paths_are_true_in_module_conditions(self):
        self.write("answer.cpp", "int answer() { return 42; }\n")
        for root in ("ARCADIA_ROOT", "ARCADIA_BUILD_ROOT"):
            with self.subTest(root=root):
                self.write("ya.make", f"LIBRARY()\nSET(E2E_PATH ${{{root}}}/path)\nIF(E2E_PATH)\nSRCS(answer.cpp)\nELSE()\nMESSAGE(FATAL_ERROR rooted_path_was_false)\nENDIF()\nEND()\n")
                compiled = self.node(self.graph(), "answer.cpp.o")
                self.assertIn(f"$(S)/{PROJECT}/answer.cpp", compiled["inputs"])

    def test_graph_with_ownership_checks(self):
        self.write("ya.make", "PROGRAM(answer)\nSRCS(main.cpp)\nRESOURCE(value.txt /value)\nEND()\n")
        self.write("main.cpp", "int main() { return 0; }\n")
        self.write("value.txt", "42\n")
        with patch.dict(os.environ, {"AY_DEBUG_OWNERSHIP": "1"}):
            self.node(self.graph(), "answer")

    def test_test_output_modes(self):
        self.write("ya.make", "PY3TEST()\nTEST_SRCS(test_answer.py)\nEND()\n")
        self.write("test_answer.py", "def test_answer():\n    assert 42 == 6 * 7\n")
        for env in (
            {"YA_DIR_OUTPUTS": "yes", "YA_DIR_OUTPUTS_IN_NODES": "yes"},
            {"YA_DIR_OUTPUTS": "no", "YA_TEST_REMOVE_TOS": "yes"},
            {"YA_AUTO_DETECT_AGENT": "no", "YA_TEST_DISABLE_FLAKE8_MIGRATIONS": "yes"},
        ):
            with self.subTest(env=env), patch.dict(os.environ, env):
                self.graph("-t", "--tests-retries=2")
