import base64
import unittest

import lib
from test_link_test_nodes import fixture


class TestRunnerOptions(unittest.TestCase):
    def test_dist_cache_environment_and_target_descriptor(self):
        graph = lib.make(fixture(), "lib/ut", "-t", "--dist", "--cache-tests", "--target-platform-flag", "AUTOCHECK=yes", env={"YA_TEST_TASK_HINT": "present", "YA_TOKEN": "not-forwarded"})
        node = lib.node_by_output(graph, "$(B)/lib/ut/test-results/unittest/meta.json")
        args = node["cmds"][0]["cmd_args"]
        self.assertIn("--truncate-files", args)
        self.assertEqual(args[args.index("--result-max-file-size") + 1], "102400")
        self.assertIn("AUTOCHECK=yes", args[args.index("--target-platform-descriptor") + 1])
        self.assertNotIn("disable_cache", node["kv"])
        self.assertNotIn("TESTING_SAVE_OUTPUT", node["env"])
        self.assertEqual(node["env"]["YA_TEST_TASK_HINT"], "present")
        self.assertNotIn("YA_TOKEN", node["env"])
        self.assertEqual(node["env"]["TSAN_OPTIONS"], "exitcode=100")
        context = lib.node_by_output(graph, "$(B)/common_test.context")
        self.assertIn('    "AUTOCHECK": "yes",', context["cmds"][0]["cmd_args"])

    def test_retries_merge_separate_runs(self):
        graph = lib.make(fixture(), "lib/ut", "-t", "--tests-retries", "2")
        runs = [lib.node_by_output(graph, f"$(B)/lib/ut/test-results/unittest/run{i}/meta.json") for i in (1, 2)]
        merged = lib.node_by_output(graph, "$(B)/lib/ut/test-results/unittest/meta.json")
        self.assertEqual(merged["kv"]["p"], "TR")
        self.assertEqual(set(merged["deps"]), {r["uid"] for r in runs})
        self.assertEqual(set(merged["inputs"]), {o for r in runs for o in r["outputs"]})
        self.assertIn(merged["uid"], graph["result"])
        self.assertTrue(all(r["uid"] not in graph["result"] for r in runs))

    def test_sandbox_data_adds_shared_download_dependency(self):
        files = fixture()
        files["lib/ut/ya.make"] = files["lib/ut/ya.make"].replace("END()", "DATA(sbr://123=data sbr://123=other)\nEND()")
        graph = lib.make(files, "lib/ut", "-t")
        download = lib.node_by_output(graph, "$(B)/sandbox-storage/123/resource")
        run = lib.node_by_output(graph, "$(B)/lib/ut/test-results/unittest/meta.json")
        self.assertIn(download["uid"], run["deps"])
        self.assertEqual(download["kv"]["p"], "DL")
        args = run["cmds"][0]["cmd_args"]
        self.assertIn("123=data", args)
        self.assertIn("123=other", args)

    def test_size_filter_and_medium_node_type(self):
        files = fixture()
        files["lib/ut/ya.make"] = files["lib/ut/ya.make"].replace("END()", "SIZE(MEDIUM)\nEND()")
        graph = lib.make(files, "lib/ut", "-t", "--test-size", "medium")
        run = lib.node_by_output(graph, "$(B)/lib/ut/test-results/unittest/meta.json")
        self.assertEqual(run["kv"]["p"], "TM")
        filtered = lib.make(files, "lib/ut", "-t", "--test-size", "small")
        self.assertFalse(any("/unittest/meta.json" in o for n in filtered["graph"] for o in n["outputs"]))

    def test_recipe_delimiters_and_run_quoting(self):
        graph = lib.make({"a/ya.make": 'EXECTEST()\nRUN(tool "two words")\nUSE_RECIPE(first start)\nUSE_RECIPE(second start)\nEND()\n'}, "a", "-t")
        run = lib.node_by_output(graph, "$(B)/a/test-results/exectest/meta.json")
        args = run["cmds"][0]["cmd_args"]
        self.assertEqual(base64.b64decode(args[args.index("--recipes") + 1]), b"first start \n second start \n")
        cmd = next(a for a in args if a.startswith("commands="))
        self.assertEqual(base64.b64decode(cmd.split("=", 1)[1]), b'\ntool "two words"')


if __name__ == "__main__":
    unittest.main(verbosity=2)
