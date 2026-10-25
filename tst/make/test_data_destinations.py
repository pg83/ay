import unittest

from tst import lib
from tst.make.test_java_tests import junit_tree


ENTRIES = (
    ("sbr://701=configs/", "701=configs"),
    ("sbr://702=outer//inner///", "702=outer//inner"),
    ("sbr://703=outer/./inner/../leaf/", "703=outer/./inner/../leaf"),
    ("sbr://704=/root//", "704=/root"),
    ("sbr://705=./", "705=."),
    ("sbr://706=https://cache//segment/./", "706=https://cache//segment/."),
    ("sbr://707=renamed.bin/", "707=renamed.bin"),
    ("sbr://708", "708"),
    ('"sbr://709=configs\\\\"', "709=configs"),
)


def suite_files(data):
    return {"grove/check/ya.make": "EXECTEST()\nRUN(true)\nDATA(" + data + ")\nEND()\n"}


def values(node, option):
    args = node["cmds"][-1]["cmd_args"]
    return [args[i + 1] for i, arg in enumerate(args) if arg == option]


class DataDestinationsTest(unittest.TestCase):
    def assert_downloads(self, graph, run, expected):
        self.assertEqual(values(run, "--sandbox-resource"), expected)
        for resource in {value.split("=", 1)[0] for value in expected}:
            output = f"$(B)/sandbox-storage/{resource}/resource"
            producers = [n for n in graph["graph"] if output in n["outputs"]]
            self.assertEqual(len(producers), 1)
            download, = producers
            self.assertIn(download["uid"], run["deps"])
            self.assertEqual(download["inputs"], [])
            self.assertEqual(values(download, "--id"), [resource])
            self.assertEqual(values(download, "--rename-to"), ["resource"])
            self.assertEqual(values(download, "--resource-file"), [f"$(RESOURCE_ROOT)/sbr/{resource}/resource"])
        ids = {node["uid"] for node in graph["graph"]}
        self.assertTrue(graph["result"])
        self.assertLessEqual(set(graph["result"]), ids)
        self.assertTrue(all(set(node["deps"]) <= ids for node in graph["graph"]))

    def test_metadata_trims_only_trailing_separators_of_the_whole_entry(self):
        for dist in (False, True):
            with self.subTest(dist=dist):
                graph = lib.make(suite_files(" ".join(raw for raw, _ in ENTRIES)), "grove/check", "-t",
                                 *(["--dist"] if dist else []))
                run = lib.node_by_output(graph, "$(B)/grove/check/test-results/exectest/meta.json")
                self.assert_downloads(graph, run, sorted(value for _, value in ENTRIES))

    def test_normalized_duplicates_and_autoupdate_keys_do_not_duplicate_consumers(self):
        graph = lib.make(suite_files("sbr://701=configs/ AUTOUPDATED optional-key "
                                    "sbr://701=configs sbr://701=configs///"), "grove/check", "-t")
        run = lib.node_by_output(graph, "$(B)/grove/check/test-results/exectest/meta.json")
        self.assert_downloads(graph, run, ["701=configs"])

    def test_local_data_keeps_source_and_copy_ownership(self):
        files = suite_files("arcadia/orchard/// arcadia/orchard/./ ext:payload.json/")
        files["orchard/leaf.txt"] = "source data\n"
        files["grove/check/payload.json"] = "{}\n"
        graph = lib.make(files, "grove/check", "-t")
        run = lib.node_by_output(graph, "$(B)/grove/check/test-results/exectest/meta.json")
        self.assertEqual(values(run, "--sandbox-resource"), [])
        self.assertIn("$(S)/orchard", run["inputs"])
        self.assertEqual(values(run, "--external-local-file"), ["grove/check/payload.json"])
        copy = lib.node_by_output(graph, "$(B)/external_local/grove/check/payload.json")
        self.assertEqual(copy["inputs"], ["$(S)/grove/check/payload.json"])
        self.assertIn(copy["uid"], run["deps"])

    def test_java_data_uses_the_same_metadata_rule_before_sorting(self):
        files = junit_tree("JUNIT5()\nDATA(sbr://701=configs/ sbr://701=configs "
                           "sbr://703=outer/./inner/../leaf/)\nEND()\n")
        graph = lib.make(files, "proj/sub/ut", "-t")
        run = lib.node_by_output(graph, "$(B)/proj/sub/ut/test-results/sub-ut/meta.json")
        self.assert_downloads(graph, run, ["701=configs", "703=outer/./inner/../leaf"])
        jar = lib.node_by_output(graph, "$(B)/proj/sub/ut/proj-sub-ut.jar")
        self.assertIn(jar["uid"], run["deps"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
