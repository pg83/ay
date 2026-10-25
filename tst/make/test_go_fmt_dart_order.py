import unittest

from tst import lib
from tst.make.test_go_fmt_sources import PEER_TESTS, fmt_files, suite_resources
from tst.make.test_go_tests import AUTOCHECK, args_after, go_test_world, resources_library, test_node


def fixture(recurse=True):
    files = go_test_world()
    resources_library(files, "resources/roots", "ROOT_PAYLOAD")
    resources_library(files, "resources/petal", "PETAL_PAYLOAD")
    files.update({
        "grove/ya.make": (
            "GO_LIBRARY()\nSRCS(grove.go)\nGO_TEST_SRCS(grove_test.go)\n"
            "PEERDIR(resources/roots)\nENV(OWNER=library)\nEND()\n"
            + ("RECURSE(gotest)\n" if recurse else "")
        ),
        "grove/grove.go": "package grove\n",
        "grove/grove_test.go": "package grove\n",
        "grove/gotest/ya.make": (
            "GO_TEST_FOR(grove)\nPEERDIR(resources/petal)\nENV(OWNER=child)\nEND()\n"
        ),
    })
    return files


class GoFmtDartOrderTest(unittest.TestCase):
    def assert_owner(self, graph, child, files):
        self.assertEqual(fmt_files(graph, "grove"), files)
        owner = "grove/gotest" if child else "grove"
        self.assertEqual(suite_resources(graph, "grove/gofmt"),
                         suite_resources(graph, owner + "/govet"))
        self.assertEqual(
            args_after(test_node(graph, "grove/gofmt")["cmds"][0]["cmd_args"], "--env"),
            args_after(test_node(graph, owner + "/govet")["cmds"][0]["cmd_args"], "--env"))
        resources = suite_resources(graph, "grove/gofmt")
        self.assertIn("ROOT_PAYLOAD_RESOURCE_GLOBAL::$(B)/resources/ROOT_PAYLOAD", resources)
        self.assertEqual("PETAL_PAYLOAD_RESOURCE_GLOBAL::$(B)/resources/PETAL_PAYLOAD" in resources,
                         child)
        command = test_node(graph, "grove/gofmt")["cmds"][0]["cmd_args"]
        self.assertEqual([arg for arg in args_after(command, "--env") if arg.startswith("OWNER=")],
                         ["OWNER=library", "OWNER=child"] if child else ["OWNER=library"])
        ids = {node["uid"] for node in graph["graph"]}
        self.assertTrue(set(graph["result"]) <= ids)
        self.assertTrue(all(set(node["deps"]) <= ids for node in graph["graph"]))

    def test_recursed_test_for_keeps_the_child_record_and_merges_only_files(self):
        graph = lib.make(fixture(), "grove", *AUTOCHECK, *PEER_TESTS,
                         "--add-modules-to-results", "--output-only-tests")
        self.assert_owner(graph, True, ["$(S)/grove/grove.go", "$(S)/grove/grove_test.go"])
        package = lib.node_by_output(graph, "$(B)/grove/grove.a")
        self.assertIn("$(S)/grove/grove.go", package["inputs"])
        self.assertNotIn("$(S)/grove/grove_test.go", package["inputs"])
        self.assertIn("$(S)/grove/grove_test.go",
                      lib.node_by_output(graph, "$(B)/grove/gotest/gotest")["inputs"])

    def test_nonrecursive_explicit_roots_keep_the_first_owner(self):
        for roots, child, files in (
            (("grove",), False, ["$(S)/grove/grove.go"]),
            (("grove/gotest",), True, ["$(S)/grove/grove_test.go"]),
            (("grove", "grove/gotest"), False,
             ["$(S)/grove/grove.go", "$(S)/grove/grove_test.go"]),
            (("grove/gotest", "grove"), True,
             ["$(S)/grove/grove.go", "$(S)/grove/grove_test.go"]),
        ):
            with self.subTest(roots=roots):
                graph = lib.make(fixture(False), roots[-1], *AUTOCHECK, *PEER_TESTS, *roots[:-1])
                self.assert_owner(graph, child, files)

    def test_shared_recursion_cycle_keeps_first_discovery_across_explicit_roots(self):
        files = fixture()
        files["grove/gotest/ya.make"] += "RECURSE(..)\n"
        files["canopy/ya.make"] = (
            "GO_LIBRARY()\nSRCS(canopy.go)\nEND()\n"
            "RECURSE(../grove/gotest ../grove ../grove/gotest)\n"
        )
        files["canopy/canopy.go"] = "package canopy\n"
        for roots, child in ((("grove", "canopy"), True), (("canopy", "grove"), False)):
            with self.subTest(roots=roots):
                graph = lib.make(files, roots[-1], *AUTOCHECK, *PEER_TESTS, *roots[:-1])
                self.assert_owner(graph, child, ["$(S)/grove/grove.go", "$(S)/grove/grove_test.go"])

    def test_disabled_test_discovery_keeps_the_ordinary_package(self):
        graph = lib.make(fixture(), "grove")
        self.assertFalse(any(node["kv"].get("run_test_node") for node in graph["graph"]))
        package = lib.node_by_output(graph, "$(B)/grove/grove.a")
        self.assertIn("$(S)/grove/grove.go", package["inputs"])
        self.assertNotIn("$(S)/grove/grove_test.go", package["inputs"])


if __name__ == "__main__":
    unittest.main()
