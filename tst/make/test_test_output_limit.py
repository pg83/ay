import unittest

from tst import lib
from tst.make.test_python_suites import fixture as python_fixture


PROJECT = "grove/check"
BODIES = {
    "ordinary": "",
    "tagged": "TAG(ya:huge_logs)\n",
    "near": "TAG(huge_logs)\n",
    "repeated": "TAG(ya:huge_logs)\nTAG(ya:huge_logs)\n",
    "requirements": "REQUIREMENTS(cpu:4 ram:9 ram_disk:2 network:full)\nTAG(ya:huge_logs)\n",
}


def fixture(mode):
    if mode in ("python", "chunked"):
        body = ('NO_PYTHON_INCLUDES()\nNO_IMPORT_TRACING()\nDISABLE(PYTHON_SQLITE3)\n'
                'ALLOCATOR(FAKE)\nTAG(ya:huge_logs)\n')
        if mode == "chunked":
            body += "SET(FLAKE8_FILE_PROCESSING_TIME 31)\nSPLIT_FACTOR(2)\nFORK_SUBTESTS()\nPY_SRCS(b.py)\n"
        files = python_fixture(body, macro="PY3TEST")
        files.update({"lib/b.py": "value = 2\n", "lint/flake8.conf": "",
                      "library/python/pytest/ya.make": f"LIBRARY()\n{lib.BARE_MODULE}END()\n"})
        return {(PROJECT + key[3:] if key.startswith("lib/") else key): value for key, value in files.items()}, [PROJECT]
    if mode == "shared":
        files = {"common/settings.inc": "IF(EXTENDED_LOGS)\nTAG(ya:huge_logs)\nENDIF()\nREQUIREMENTS(cpu:4 ram:32 ram_disk:2)\n"}
        for directory, enabled in (("grove/left", "yes"), ("grove/right", "no")):
            files[directory + "/ya.make"] = (f"EXECTEST()\nRUN(true)\nSET(EXTENDED_LOGS {enabled})\n"
                                             "INCLUDE(${ARCADIA_ROOT}/common/settings.inc)\nREQUIREMENTS(ram:9)\nEND()\n")
        return files, ["grove/left", "grove/right"]
    return {PROJECT + "/ya.make": "EXECTEST()\nRUN(true)\n" + BODIES[mode] + "END()\n"}, [PROJECT]


def test_runs(graph):
    return [node for node in graph["graph"] if node["kv"].get("run_test_node")]


class TestOutputLimitTest(unittest.TestCase):
    def test_huge_logs_tag_adds_the_upstream_numeric_byte_limit(self):
        for mode in BODIES:
            with self.subTest(mode=mode):
                files, targets = fixture(mode)
                graph = lib.make(files, *targets, "-t")
                run, = test_runs(graph)
                expected = {"cpu": 1, "network": "restricted", "ram": 8, "ram_disk": 0}
                if mode == "requirements":
                    expected = {"cpu": 4, "network": "full", "ram": 9, "ram_disk": 2}
                if mode not in ("ordinary", "near"):
                    expected["test_output_limit"] = 1024 ** 3
                    self.assertIs(type(run["requirements"]["test_output_limit"]), int)
                self.assertEqual(run["requirements"], expected)

    def test_shared_settings_keep_the_limit_local_to_the_tagged_suite(self):
        files, targets = fixture("shared")
        graph = lib.make(files, *targets, "-t")
        for run in test_runs(graph):
            expected = {"cpu": 4, "network": "restricted", "ram": 9, "ram_disk": 2}
            if run["outputs"][0].startswith("$(B)/grove/left/"):
                expected["test_output_limit"] = 1024 ** 3
            self.assertEqual(run["requirements"], expected)
        self.assertEqual(len(test_runs(graph)), 2)

    def test_real_chunks_keep_the_limit_without_tagging_import_style_or_build_nodes(self):
        for mode in ("python", "chunked"):
            with self.subTest(mode=mode):
                files, targets = fixture(mode)
                graph = lib.make(files, *targets, "-t")
                for run in test_runs(graph):
                    if "/test-results/py3test/" in run["outputs"][0]:
                        self.assertEqual(run["requirements"]["test_output_limit"], 1024 ** 3)
                    else:
                        self.assertNotIn("test_output_limit", run["requirements"])
                for node in graph["graph"]:
                    if node["kv"].get("p") != "TS":
                        self.assertNotIn("test_output_limit", node["requirements"])
                compiled = lib.node_by_output(graph, f"$(B)/{PROJECT}/a.py.yapyc3")
                self.assertIn(f"$(S)/{PROJECT}/a.py", compiled["inputs"])
                program = lib.node_by_output(graph, f"$(B)/{PROJECT}/grove-check")
                self.assertTrue(program["inputs"])
                ids = {node["uid"] for node in graph["graph"]}
                self.assertTrue(set(graph["result"]) <= ids)


if __name__ == "__main__":
    unittest.main(verbosity=2)
