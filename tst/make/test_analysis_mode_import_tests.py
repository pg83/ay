import json

from tst.make.lib import MakeTest


class AnalysisModeImportTestsTest(MakeTest):
    def test_cpp_analysis_mode_omits_python_program_and_import_check_targets(self):
        self.write("ya.make", "PY3_PROGRAM(answer)\nPY_SRCS(MAIN main.py)\nEND()\n")
        self.write("main.py", "print(42)\n")
        result = self.make("-t", "-DCPP_ANALYSIS_MODE=yes")
        self.assertEqual(result.returncode, 0, result.stderr)
        graph = json.loads(result.stdout)
        self.assertFalse(any("/test-results/import_test/" in output for node in graph["graph"] for output in node["outputs"]))
        self.assertEqual(graph["result"], [])
