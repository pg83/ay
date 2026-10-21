import json
import unittest

import lib


KEY = "12/abcdef/resource.tar.gz"
OUTPUT = "$(B)/canondata_storage/" + KEY


def fixture(data=None, body="", canon="canondata/result.json"):
    files = {"test/ya.make": "EXECTEST()\nRUN(true)\n" + body + "END()\n",
             "ya.conf": "default_node_requirements = '{\"cpu\": 1, \"ram\": 32, \"network\": \"restricted\"}'\n"}
    if data is not None:
        files["test/" + canon] = json.dumps(data)
    return files


class CanonicalDataTest(unittest.TestCase):
    def test_nested_canonical_resources_share_one_download_across_chunks(self):
        files = fixture({"first": [{"uri": "https://storage.yandex-team.ru/get-devtools/" + KEY + "#first", "checksum": "abc"}],
                         "second": {"uri": "https://{canondata_backend}/" + KEY + "#second", "checksum": "def"},
                         "ignored": {"uri": "https://storage.yandex-team.ru/get-devtools/other"}}, "SPLIT_FACTOR(2)\n")
        graph = lib.make(files, "test", "-t", "--dist")
        download = lib.node_by_output(graph, OUTPUT)
        self.assertEqual(sum(OUTPUT in n["outputs"] for n in graph["graph"]), 1)
        self.assertEqual(download["outputs"], [OUTPUT, OUTPUT + ".log"])
        self.assertEqual(download["inputs"], ["$(S)/build/scripts/fetch_from.py", "$(S)/build/scripts/fetch_from_mds.py"])
        self.assertEqual(download["cmds"][0]["cmd_args"], ["$(YMAKE_PYTHON3)/bin/python3", "$(S)/build/scripts/fetch_from_mds.py", "--key", KEY,
                         "--entrypoint", "storage.yandex-team.ru/get-devtools", "--rename-to", OUTPUT, "--log-path", OUTPUT + ".log", "--scheme", "https"])
        self.assertEqual(download["requirements"], {"cpu": 1, "ram": 32, "network": "full"})
        for index in range(2):
            run = lib.node_by_output(graph, f"$(B)/test/test-results/exectest/chunk{index}/meta.json")
            self.assertIn(download["uid"], run["deps"])
            self.assertNotIn(OUTPUT, run["inputs"])

    def test_distinct_suites_reuse_one_canonical_download(self):
        data = {"uri": "https://storage.yandex-team.ru/get-devtools/" + KEY, "checksum": "abc"}
        files = fixture(data)
        for relative, value in list(files.items()):
            if relative.startswith("test/"):
                files["other/" + relative] = value
        graph = lib.make(files, "test", "-t", "other/test")
        download = lib.node_by_output(graph, OUTPUT)
        self.assertEqual(sum(OUTPUT in n["outputs"] for n in graph["graph"]), 1)
        for project in ("test", "other/test"):
            run = lib.node_by_output(graph, "$(B)/" + project + "/test-results/exectest/meta.json")
            self.assertIn(download["uid"], run["deps"])

    def test_mds_download_uses_local_python_when_arcadia_python_is_disabled(self):
        graph = lib.make(fixture(body="DATA(mds:" + KEY + ")\n"), "test", "-t", "-DUSE_ARCADIA_PYTHON=no")
        self.assertEqual(lib.node_by_output(graph, OUTPUT)["cmds"][0]["cmd_args"][0], "python3")

    def test_selected_canondata_subpath_and_root_array_are_supported(self):
        files = fixture([{"uri": "https://storage.yandex-team.ru/get-devtools/" + KEY, "checksum": "abc"}],
                        "SET(CANONIZE_SUB_PATH selected)\n", "canondata/selected/result.json")
        files["test/canondata/result.json"] = '{"uri":"https://storage.yandex-team.ru/get-devtools/unused","checksum":"x"}'
        graph = lib.make(files, "test", "-t")
        lib.node_by_output(graph, OUTPUT)
        self.assertFalse(any("$(B)/canondata_storage/unused" in n["outputs"] for n in graph["graph"]))

    def test_canonical_sandbox_resources_reuse_declared_data_downloads(self):
        for declared in (False, True):
            with self.subTest(declared=declared):
                graph = lib.make(fixture({"uri": "sbr://123#item", "checksum": "abc"}, "DATA(sbr://123=declared)\n" if declared else ""), "test", "-t")
                download = lib.node_by_output(graph, "$(B)/sandbox-storage/123/resource")
                run = lib.node_by_output(graph, "$(B)/test/test-results/exectest/meta.json")
                self.assertEqual(run["deps"].count(download["uid"]), 1)
                self.assertEqual(sum("$(B)/sandbox-storage/123/resource" in n["outputs"] for n in graph["graph"]), 1)
                self.assertIn("123=canondata_storage/123", run["cmds"][0]["cmd_args"])

    def test_explicit_mds_data_uses_the_same_download_mechanism(self):
        graph = lib.make(fixture(body="DATA(mds:" + KEY + ")\n"), "test", "-t")
        download = lib.node_by_output(graph, OUTPUT)
        run = lib.node_by_output(graph, "$(B)/test/test-results/exectest/meta.json")
        self.assertIn(download["uid"], run["deps"])

    def test_go_and_java_runtime_fields_collect_canonical_resources(self):
        from test_go_tests import go_test_world
        from test_java_tests import junit_tree
        go = go_test_world()
        go["go/ya.make"] = "GO_TEST()\nGO_TEST_SRCS(a_test.go)\nEND()\n"
        go["go/a_test.go"] = "package example\n"
        java = junit_tree("JUNIT5()\nEND()\n")
        for files, project, suite in ((go, "go", "go"), (java, "proj/sub/ut", "sub-ut")):
            with self.subTest(project=project):
                files[project + "/canondata/result.json"] = json.dumps({"uri": "https://storage.yandex-team.ru/get-devtools/" + KEY, "checksum": "abc"})
                graph = lib.make(files, project, "-t")
                download = lib.node_by_output(graph, OUTPUT)
                run = lib.node_by_output(graph, "$(B)/" + project + "/test-results/" + suite + "/meta.json")
                self.assertIn(download["uid"], run["deps"])

    def test_native_test_fields_do_not_implicitly_collect_canonical_resources(self):
        from test_link_test_nodes import fixture as cpp_fixture
        from test_fuzz_metadata import fixture as fuzz_fixture
        for files, project in ((cpp_fixture(), "lib/ut"), (fuzz_fixture(), "app/fuzz")):
            with self.subTest(project=project):
                files[project + "/canondata/result.json"] = '{"uri": 1, "checksum": "abc"}'
                result = lib.make_process(files, project, "-t")
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertFalse(any(n.get("kv", {}).get("p") == "DL" for n in json.loads(result.stdout)["graph"]))

    def test_canonical_resource_uri_requires_a_string(self):
        result = lib.make_process(fixture({"uri": 1, "checksum": "abc"}), "test", "-t")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("canonical resource URI must be a string", result.stderr)

    def test_malformed_canondata_reports_a_diagnostic_without_failing_generation(self):
        files = fixture()
        files["test/canondata/result.json"] = "{broken"
        result = lib.make_process(files, "test", "-t")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("malformed canonical data", result.stderr)
        self.assertFalse(any(n.get("kv", {}).get("p") == "DL" for n in json.loads(result.stdout)["graph"]))


if __name__ == "__main__":
    unittest.main(verbosity=2)
