"""Declared DATA survives the shared TS check-suite metadata composition."""
import unittest

from tst import lib
from tst.make import test_nots
from tst.make.test_java_tests import option_values


WRAPPER_PATHS = ["$(S)/cedar/index.js", "$(S)/cedar/package.json", "$(S)/cedar/ya.make"]


def fixture():
    files = {("cedar/" + k[4:] if k.startswith("web/") else k): v
             for k, v in test_nots.NotsTests().project().items()}
    files["cedar/ya.make"] = (
        "TS_LIBRARY()\nTS_BUILD_SCRIPT(emit)\nTS_BUILD_OUTPUTS(out)\n"
        "TS_TEST(inline:check)\nTS_LINT(inline:lint)\nSET(CASE_FILE fixtures/inline.json)\n"
        "INCLUDE(${ARCADIA_ROOT}/policy/data.inc)\nEND()\n"
    )
    files["cedar/package.json"] = '{"name":"cedar","scripts":{"emit":"echo build","inline:check":"echo test"}}'
    files["policy/data.inc"] = (
        "DATA(arcadia/${CASE_FILE} arcadia/fixtures/tree arcadia/cedar/package.json "
        "arcadia/tools/emitter/seed.json arcadia/fixtures/tree sbr://881)\n"
        'ENV("SERVICE_ROOT=$(SOURCE_ROOT)/not-data")\nDEPENDS(tools/emitter)\n'
    )
    for name in ("alpha", "beta"):
        files[f"checks/{name}/ya.make"] = (
            "TS_TEST_FOR(cedar)\nTS_TEST(first:check)\nTS_TEST(second:check)\n"
            f"TS_TEST_DEPENDS_ON_BUILD()\nSET(CASE_FILE fixtures/{name}.json)\n"
            "INCLUDE(${ARCADIA_ROOT}/policy/data.inc)\nEND()\n"
        )
    files.update({
        "fixtures/alpha.json": "alpha", "fixtures/beta.json": "beta", "fixtures/inline.json": "inline",
        "fixtures/ordinary.json": "ordinary", "fixtures/tree/leaf.txt": "tree",
        "not-data/ignored.json": "environment text is not a data declaration",
        "tools/emitter/seed.json": "source data alongside a real build producer",
        "ordinary/ya.make": "EXECTEST()\nEXPLICIT_DATA()\nRUN(check)\n"
                            "SET(CASE_FILE fixtures/ordinary.json)\n"
                            "INCLUDE(${ARCADIA_ROOT}/policy/data.inc)\nEND()\n",
    })
    lib.tool_program(files, "tools/emitter", "renamed-emitter")
    return files


def make(files, *roots):
    return lib.make(files, *(roots or ("checks/alpha",)), "--target-platform-tests", "-DAUTOCHECK=yes")


def suite(graph, project, name):
    return lib.node_by_output(graph, f"$(B)/{project}/test-results/{name}/meta.json")


def paths(node):
    return option_values(node["cmds"][-1]["cmd_args"], "--test-related-path")


def declared(owner):
    return [f"$(S)/fixtures/{owner}.json", "$(S)/fixtures/tree", "$(S)/tools/emitter/seed.json"]


class NotsDeclaredDataTest(unittest.TestCase):
    def assert_test_data(self, graph, node, owner, wrappers=WRAPPER_PATHS):
        expected = wrappers + declared(owner)
        self.assertEqual(paths(node), expected)
        self.assertEqual(node["inputs"], sorted(expected))
        self.assertNotIn("$(S)/fixtures/tree/leaf.txt", node["inputs"])
        self.assertNotIn("$(S)/not-data", node["inputs"])
        self.assertNotIn("$(S)/881", node["inputs"])
        args = node["cmds"][-1]["cmd_args"]
        self.assertEqual(option_values(args, "--sandbox-resource"), ["881"])
        sandbox = lib.node_by_output(graph, "$(B)/sandbox-storage/881/resource")
        tool = lib.node_by_output(graph, "$(B)/tools/emitter/renamed-emitter")
        self.assertIn(sandbox["uid"], node["deps"])
        self.assertIn(tool["uid"], node["deps"])
        self.assertNotIn(tool["outputs"][0], paths(node))
        compiled = lib.node_by_output(graph, "$(B)/tools/emitter/main.cpp.o")
        self.assertIn("$(S)/tools/emitter/main.cpp", compiled["inputs"])
        self.assertIn(compiled["outputs"][0], tool["inputs"])
        self.assertIn(compiled["uid"], tool["deps"])
        self.assertTrue(any("SERVICE_ROOT=" in v and "/not-data" in v
                            for v in option_values(args, "--env")))

    def test_each_test_label_retains_declared_files_directories_and_real_dependencies(self):
        graph = make(fixture())
        prepare = lib.node_by_output(graph, "$(B)/cedar/package.json")
        builder = lib.node_by_output(graph, "$(B)/devtools/frontend_build_platform/nots/builder/builder")
        built = lib.node_by_output(graph, "$(B)/cedar/output.tar")
        module = lib.node_by_output(graph, "$(B)/checks/alpha/checks-alpha")
        self.assertNotIn(built["uid"], module["deps"])
        self.assertNotIn(built["outputs"][0], module["inputs"])
        self.assertIn(builder["uid"], prepare["deps"])
        for label in ("first", "second"):
            node = suite(graph, "checks/alpha", f"ts_test_{label}_check")
            self.assert_test_data(graph, node, "alpha")
            self.assertIn(prepare["uid"], node["deps"])
            self.assertIn(builder["uid"], node["deps"])
            self.assertEqual(node["deps"].count(built["uid"]), 1)
            self.assertNotIn(built["outputs"][0], node["inputs"])
            self.assertEqual(option_values(node["cmds"][-1]["cmd_args"], "--script-name"), [label + ":check"])

    def test_shared_build_checks_keep_data_but_lint_omits_test_metadata(self):
        graph = make(fixture(), "cedar")
        node = suite(graph, "cedar", "ts_test_inline_check")
        self.assert_test_data(graph, node, "inline")
        lint = suite(graph, "cedar", "ts_lint_inline_lint")
        self.assertEqual(paths(lint), WRAPPER_PATHS)
        self.assertEqual(lint["inputs"], WRAPPER_PATHS)
        args = lint["cmds"][-1]["cmd_args"]
        self.assertEqual(option_values(args, "--sandbox-resource"), [])
        self.assertEqual(option_values(args, "--env"), [])
        self.assertNotIn(lib.node_by_output(graph, "$(B)/sandbox-storage/881/resource")["uid"], lint["deps"])

    def test_shared_lexical_data_keeps_contexts_and_root_order(self):
        observations = []
        for roots in (("checks/alpha", "checks/beta"), ("checks/beta", "checks/alpha")):
            graph = make(fixture(), *roots)
            outputs_by_uid = {n["uid"]: tuple(n["outputs"]) for n in graph["graph"]}
            nodes = {}
            for owner in ("alpha", "beta"):
                for label in ("first", "second"):
                    node = suite(graph, "checks/" + owner, "ts_test_" + label + "_check")
                    self.assert_test_data(graph, node, owner)
                    # Graph dumps expose allocation references as UIDs. Check
                    # every dependency's real outputs across discovery orders.
                    nodes[owner, label] = (node["cmds"], node["inputs"], node["outputs"],
                                           sorted(outputs_by_uid[dep] for dep in node["deps"]))
            observations.append(nodes)
        self.assertEqual(observations[0], observations[1])

    def test_source_data_matches_ordinary_context_and_tracks_the_declared_identity(self):
        files = fixture()
        graph = make(files, "ordinary", "checks/alpha")
        ordinary = suite(graph, "ordinary", "exectest")
        # Ordinary tests have the same source DATA plus the declared overlap
        # with the frontend manifest, and no frontend wrapper-derived paths.
        self.assert_test_data(graph, ordinary, "ordinary", ["$(S)/cedar/package.json"])
        original = suite(graph, "checks/alpha", "ts_test_first_check")
        changed = {**files, "fixtures/replacement.json": "replacement"}
        changed["checks/alpha/ya.make"] = files["checks/alpha/ya.make"].replace(
            "SET(CASE_FILE fixtures/alpha.json)", "SET(CASE_FILE fixtures/replacement.json)")
        updated = make(changed, "ordinary", "checks/alpha")
        current = suite(updated, "checks/alpha", "ts_test_first_check")
        self.assert_test_data(updated, current, "replacement")
        self.assertIn("$(S)/fixtures/alpha.json", original["inputs"])
        self.assertNotIn("$(S)/fixtures/alpha.json", current["inputs"])
        self.assertEqual(ordinary, suite(updated, "ordinary", "exectest"))
        self.assertEqual(lib.node_by_output(graph, "$(B)/cedar/package.json"),
                         lib.node_by_output(updated, "$(B)/cedar/package.json"))

    def test_normalized_data_identities_are_sorted_after_wrapper_paths(self):
        files = fixture()
        files["shadows/keep.txt"] = "real directory used by the source path"
        files["checks/alpha/ya.make"] = files["checks/alpha/ya.make"].replace(
            "SET(CASE_FILE fixtures/alpha.json)", "SET(CASE_FILE shadows/../fixtures/alpha.json)")
        graph = make(files)
        node = suite(graph, "checks/alpha", "ts_test_first_check")
        self.assert_test_data(graph, node, "alpha")


if __name__ == "__main__":
    unittest.main(verbosity=2)
