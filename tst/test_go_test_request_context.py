"""Test selection and the global source-visible test request are separate."""
import unittest
import json
from collections import Counter

import lib
from test_go_tests import between, go_module, go_test_world, go_tool_args


SCOPE = "vendor/example.net/scope/scope.a"
HOST_SCOPE = "host/scope/scope.a"
TEST_ONLY = "vendor/example.net/testonly/testonly.a"


def fixture():
    files = go_test_world()
    files.update({
        "policy/request.inc": "IF(TESTS_REQUESTED)\nPEERDIR($CONDITIONAL_PEER)\nENDIF()\n",
        "grove/ya.make": (
            "GO_LIBRARY()\nSRCS(grove.go)\nGO_TEST_SRCS(grove_test.go)\n"
            "SET(CONDITIONAL_PEER vendor/example.net/scope)\n"
            "INCLUDE(${ARCADIA_ROOT}/policy/request.inc)\nEND()\nRECURSE_FOR_TESTS(check)\n"
        ),
        "grove/grove.go": "package grove\n",
        "grove/grove_test.go": 'package grove\nimport (\n "example.net/scope"\n "example.net/testonly"\n)\n',
        "grove/check/ya.make": "GO_TEST_FOR(grove)\nEND()\n",
        "tools/go_test_miner/ya.make": (
            "GO_PROGRAM(renamed-miner)\nSRCS(go_test_miner.go)\n"
            "SET(CONDITIONAL_PEER host/scope)\n"
            "INCLUDE(${ARCADIA_ROOT}/policy/request.inc)\nEND()\n"
        ),
    })
    for path in ("vendor/example.net/scope", "vendor/example.net/testonly", "host/scope"):
        go_module(files, path)
    go_module(files, "canopy", 'import "a.yandex-team.ru/grove"\n')
    return files


def make(files, targets, *flags):
    return lib.make(files, *targets, *flags, "--target-platform", "default-linux-aarch64")


def output(graph, path):
    return lib.node_by_output(graph, "$(B)/" + path)


def graph_contents(graph):
    # Graph-dump IDs are allocation indices, including self_uid. Compare all
    # fields after relocating those indices to unique output/context identities.
    identities = {n["uid"]: (tuple(n["outputs"]), n["platform"]) for n in graph["graph"]}
    assert len(identities) == len(graph["graph"])
    assert len(set(identities.values())) == len(graph["graph"])
    assert all(ref in identities for ref in graph["result"])
    contents = []
    for node in graph["graph"]:
        assert node["self_uid"] in identities
        assert all(ref in identities for ref in node["deps"])
        relocated = dict(node)
        relocated["uid"] = identities[node["uid"]]
        relocated["self_uid"] = identities[node["self_uid"]]
        relocated["deps"] = sorted(identities[ref] for ref in node["deps"])
        if "foreign_deps" in node:
            assert all(ref in identities for refs in node["foreign_deps"].values() for ref in refs)
            relocated["foreign_deps"] = {role: sorted(identities[ref] for ref in refs)
                                         for role, refs in node["foreign_deps"].items()}
        contents.append(json.dumps(relocated, sort_keys=True))
    return dict(graph, graph=Counter(contents), result=Counter(identities[ref] for ref in graph["result"]))


class GoTestRequestContextTest(unittest.TestCase):
    def assert_contexts(self, graph, target_requested, host_requested):
        scope = output(graph, SCOPE)
        test_only = output(graph, TEST_ONLY)
        grove = output(graph, "grove/grove.a")
        canopy = output(graph, "canopy/canopy.a")
        binary = output(graph, "grove/check/check")
        miner = output(graph, "tools/go_test_miner/renamed-miner")
        scope_path = scope["outputs"][0]
        for node, source in ((grove, "grove/grove.go"), (canopy, "canopy/canopy.go")):
            self.assertIn("$(S)/" + source, node["inputs"])
            self.assertEqual(scope_path in node["inputs"], target_requested)
            self.assertEqual(scope["uid"] in node["deps"], target_requested)
            self.assertNotIn(test_only["outputs"][0], node["inputs"])
            self.assertNotIn("$(S)/grove/grove_test.go", node["inputs"])
        self.assertEqual(between(go_tool_args(grove), "++peers", "--ya-end-command-file"),
                         [SCOPE] if target_requested else [])
        self.assertEqual(between(go_tool_args(canopy), "++peers", "--ya-end-command-file"),
                         ["grove/grove.a"])
        self.assertIn(grove["outputs"][0], canopy["inputs"])
        self.assertIn(grove["uid"], canopy["deps"])
        for peer in (scope, test_only, miner):
            self.assertIn(peer["outputs"][0], binary["inputs"])
            self.assertIn(peer["uid"], binary["deps"])
        self.assertIn("$(S)/grove/grove.go", binary["inputs"])
        self.assertIn("$(S)/grove/grove_test.go", binary["inputs"])
        args = go_tool_args(binary)
        peers = between(args, "++peers", "++non-local-peers")
        self.assertIn(SCOPE, peers)
        self.assertIn(TEST_ONLY, peers)
        self.assertNotIn("grove/grove.a", peers)
        self.assertEqual(between(args, "++test_srcs", "++xtest_srcs"), ["$(S)/grove/grove_test.go"])
        self.assertEqual(args[args.index("++test-import-path") + 1], "grove")
        self.assertEqual(args[args.index("++test-miner") + 1], miner["outputs"][0])
        self.assertEqual(between(go_tool_args(miner), "++peers", "++non-local-peers")[-1:] == [HOST_SCOPE],
                         host_requested)
        self.assertEqual("$(B)/" + HOST_SCOPE in miner["inputs"], host_requested)
        self.assertIn("$(S)/tools/go_test_miner/go_test_miner.go", miner["inputs"])
        if host_requested:
            host_scope = output(graph, HOST_SCOPE)
            self.assertIn(host_scope["uid"], miner["deps"])

    def test_platform_selection_keeps_source_request_absent_in_both_root_orders(self):
        nodes = []
        for roots in (("canopy", "grove/check"), ("grove/check", "canopy")):
            with self.subTest(roots=roots):
                graph = make(fixture(), roots, "--target-platform-tests")
                nodes.append(graph_contents(graph))
                self.assert_contexts(graph, target_requested=False, host_requested=False)
        self.assertEqual(nodes[0], nodes[1])

    def test_global_request_reaches_host_and_target_in_both_root_orders(self):
        nodes = []
        for roots in (("canopy", "grove/check"), ("grove/check", "canopy")):
            with self.subTest(roots=roots):
                graph = make(fixture(), roots, "-t")
                nodes.append(graph_contents(graph))
                self.assert_contexts(graph, target_requested=True, host_requested=True)
        self.assertEqual(nodes[0], nodes[1])

    def test_explicit_flags_retain_their_host_and_target_scopes(self):
        for flags, target, host in (
            (("--target-platform-flag", "TESTS_REQUESTED=yes"), True, False),
            (("--host-platform-flag", "TESTS_REQUESTED=yes"), False, True),
            (("-DTESTS_REQUESTED=yes",), True, False),
            (("-DTESTS_REQUESTED=no",), False, False),
        ):
            with self.subTest(flags=flags):
                graph = make(fixture(), ("canopy", "grove/check"), "--target-platform-tests", *flags)
                self.assert_contexts(graph, target_requested=target, host_requested=host)

    def test_direct_test_context_keeps_real_test_imports_without_test_selection(self):
        graph = make(fixture(), ("grove/check", "canopy"))
        self.assert_contexts(graph, target_requested=False, host_requested=False)
        self.assertFalse(any(n["kv"].get("run_test_node") for n in graph["graph"]))

    def test_platform_selection_still_traverses_and_emits_test_suites(self):
        graph = make(fixture(), ("grove",), "--target-platform-tests")
        output(graph, "grove/check/check")
        grove = output(graph, "grove/grove.a")
        self.assertNotIn("$(B)/" + SCOPE, grove["inputs"])
        self.assertTrue(any(n["kv"].get("run_test_node") and n["kv"].get("path") == "grove/check/check"
                            for n in graph["graph"]))
        plain = make(fixture(), ("grove",))
        self.assertFalse(any("$(B)/grove/check/check" in n["outputs"] for n in plain["graph"]))

    def test_source_request_is_generic_for_native_modules(self):
        files = {
            "reed/ya.make": "LIBRARY()\n" + lib.BARE_MODULE +
                            "SRCS(base.cpp)\nIF(TESTS_REQUESTED)\nSRCS(requested.cpp)\nENDIF()\nEND()\n",
            "reed/base.cpp": "int base;\n",
            "reed/requested.cpp": "int requested;\n",
        }
        for flags, requested in ((("--target-platform-tests",), False), (("-t",), True)):
            with self.subTest(flags=flags):
                graph = make(files, ("reed",), *flags)
                archive = output(graph, "reed/libreed.a")
                self.assertEqual("$(B)/reed/requested.cpp.o" in archive["inputs"], requested)
                base = output(graph, "reed/base.cpp.o")
                self.assertIn("$(S)/reed/base.cpp", base["inputs"])
                self.assertIn(base["uid"], archive["deps"])
                if requested:
                    request = output(graph, "reed/requested.cpp.o")
                    self.assertIn(request["uid"], archive["deps"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
