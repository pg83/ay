import json

from tst.make.lib import MakeTest


class CanonicalDataBoundariesTest(MakeTest):
    def python_test(self, directory=""):
        self.write(directory + "ya.make", "PY3TEST()\nTEST_SRCS(test_answer.py)\nEND()\n")
        self.write(directory + "test_answer.py", "def test_answer():\n    assert 6 * 7 == 42\n")

    def test_malformed_canonical_json_does_not_prevent_suite_generation(self):
        self.python_test()
        self.write("canondata/result.json", '{"test_answer.test_answer":')
        self.node(self.graph("-t"), "test-results/py3test/meta.json")

    def test_canonical_resource_uris_require_strings(self):
        self.python_test()
        self.write("canondata/result.json", json.dumps({"test_answer.test_answer": {"uri": 42, "checksum": "a1b2"}}))
        self.assertNotEqual(self.make("-t").returncode, 0)

    def test_suites_share_canonical_downloads_with_system_python(self):
        self.write("ya.make", "RECURSE(first second)\n")
        for directory in ("first/", "second/"):
            self.write(directory + "ya.make", "EXECTEST()\nRUN(/bin/echo 42)\nEND()\n")
            self.write(directory + "canondata/result.json", json.dumps({"test_answer.test_answer": {
                "uri": "https://storage.yandex-team.ru/get-devtools/123456/answer", "checksum": "a1b2"}}))
        graph = self.graph("-t", "-DUSE_ARCADIA_PYTHON=no", "-DOS_SDK=local",
                           "--host-platform-flag=OS_SDK=local")
        downloads = [node for node in graph["graph"] if any(
            arg.endswith("/fetch_from_mds.py") for command in node["cmds"] for arg in command["cmd_args"])]
        self.assertEqual(len(downloads), 1)
        args = downloads[0]["cmds"][0]["cmd_args"]
        self.assertEqual(args[0], "python3")
        for directory in ("first", "second"):
            suite = self.node(graph, directory + "/test-results/exectest/meta.json")
            self.assertIn(downloads[0]["uid"], suite["deps"])
