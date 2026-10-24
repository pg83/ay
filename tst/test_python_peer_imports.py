import json
import unittest

import lib
from resource_dependencies import global_resource_dependencies
from test_python_srcs import python_base


WRITER = "orchard/writer"
BINARY = "$(B)/orchard/writer/writer"


def fixture(peer=True, tool=True):
    files = python_base()
    files.update({
        "parcel/ya.make": "PACKAGE(parcel)\nPEERDIR(garden/relay)\nEND()\n",
        "garden/relay/ya.make": "UNION(relay)\n" + ("PEERDIR(orchard/writer)\n" if peer else "")
                               + ("RUN_PROGRAM(orchard/writer OUT payload.json)\n" if tool else "") + "END()\n",
        "orchard/writer/ya.make": "PY3_PROGRAM(writer)\n" + lib.BARE_MODULE
                                  + "NO_PYTHON_INCLUDES()\nNO_IMPORT_TRACING()\nDISABLE(PYTHON_SQLITE3)\n"
                                  "ALLOCATOR(FAKE)\nPY_SRCS(MAIN main.py)\nEND()\n",
        "orchard/writer/main.py": "print(17)\n",
    })
    return files


class PythonPeerImportsTest(unittest.TestCase):
    def make(self, files, *roots, flags=()):
        process = lib.make_process(files, roots[0], *roots[1:], "-t", "--output-only-tests", "--strip-packages-from-results",
                                   "--target-platform-flag", "ADD_PEERDIRS_GEN_TESTS=yes", *flags)
        self.assertEqual(process.returncode, 0, process.stderr)
        graph = json.loads(process.stdout)
        index = {node["uid"]: node for node in graph["graph"]}
        self.assertEqual(len(index), len(graph["graph"]))
        self.assertTrue(set(graph["result"]) <= index.keys())
        for node in graph["graph"]:
            self.assertTrue(set(node["deps"]) <= index.keys())
            for refs in node.get("foreign_deps", {}).values():
                self.assertTrue(set(refs) <= index.keys())
        return graph, process.stderr

    def assert_source_lint(self, graph):
        suite = lib.node_by_output(graph, "$(B)/orchard/writer/test-results/flake8/meta.json")
        self.assertCountEqual(suite["inputs"], [
            "$(S)/orchard/writer", "$(S)/orchard/writer/main.py",
            "$(S)/build/config/tests/flake8/flake8.conf", "$(S)/tools/flake8_linter/__main__.py",
        ])
        context = lib.node_by_output(graph, "$(B)/common_test.context")
        resources = global_resource_dependencies(self, graph, suite, [("FLAKE8_PY3", "sbr:123")])
        self.assertEqual(suite["deps"], [context["uid"], *resources])
        self.assertEqual(suite["cmds"][0]["cmd_args"][-1], "$(S)/orchard/writer/main.py")
        return suite

    def test_union_peer_lint_does_not_materialize_an_import_binary(self):
        for tool in (False, True):
            with self.subTest(tool=tool):
                graph, stderr = self.make(fixture(tool=tool), "garden/relay")
                lint = self.assert_source_lint(graph)
                self.assertEqual(graph["result"], [lint["uid"]])
                self.assertFalse(any("/import_test/" in output or output == BINARY
                                     or output.endswith((".yapyc3", ".global.a"))
                                     for node in graph["graph"] for output in node["outputs"]))
                self.assertIn("test-dependency:", stderr)
                self.assertIn("orchard/writer", stderr)
                self.assertNotIn("module-failed:", stderr)

    def test_tool_only_and_disabled_peer_tests_have_no_suites(self):
        for files, flags in ((fixture(peer=False), ()),
                             (fixture(), ("--target-platform-flag", "ADD_PEERDIRS_GEN_TESTS=no"))):
            with self.subTest(flags=flags):
                graph, stderr = self.make(files, "garden/relay", flags=flags)
                self.assertEqual(graph["graph"], [])
                self.assertEqual(graph["result"], [])
                self.assertNotIn("test-dependency:", stderr)

    def test_unselected_import_does_not_report_an_unresolved_dependency(self):
        graph, stderr = self.make(fixture(), "garden/relay", flags=("--test-size", "medium"))
        self.assertEqual(graph["graph"], [])
        self.assertEqual(graph["result"], [])
        self.assertNotIn("test-dependency:", stderr)

    def test_dependency_warning_does_not_enable_unrelated_quiet_warnings(self):
        files = fixture(tool=False)
        files["build/sysincl/misc.yml"] = "- bogus: 1\n  includes:\n  - unused.h\n"
        graph, stderr = self.make(files, "garden/relay")
        lint = self.assert_source_lint(graph)
        self.assertEqual(graph["result"], [lint["uid"]])
        self.assertIn("test-dependency:", stderr)
        self.assertNotIn("sysincl:", stderr)

    def test_explicit_program_roots_keep_the_complete_import_dependencies(self):
        for roots in (("garden/relay", WRITER), (WRITER, "garden/relay"), ("parcel",)):
            with self.subTest(roots=roots):
                graph, stderr = self.make(fixture(), *roots)
                lint = self.assert_source_lint(graph)
                suite = lib.node_by_output(graph, "$(B)/orchard/writer/test-results/import_test/meta.json")
                binary = lib.node_by_output(graph, BINARY)
                archive = lib.node_by_output(graph, "$(B)/orchard/writer/libpy3writer.global.a")
                context = lib.node_by_output(graph, "$(B)/common_test.context")
                resources = global_resource_dependencies(self, graph, suite, [("FLAKE8_PY3", "sbr:123")])
                self.assertCountEqual(suite["deps"], [binary["uid"], archive["uid"], context["uid"], *resources])
                expected_results = [lint["uid"], suite["uid"]]
                if WRITER in roots:
                    expected_results.append(binary["uid"])
                self.assertCountEqual(graph["result"], expected_results)
                self.assertEqual(suite["inputs"], [BINARY, "$(S)/orchard/writer"])
                self.assertEqual(suite["cmds"][0]["cmd_args"][-2:], [BINARY, "--no-snippet-from-stdout"])
                self.assertIn(archive["uid"], binary["deps"])
                bytecode = lib.node_by_output(graph, "$(B)/orchard/writer/main.py.yapyc3")
                self.assertIn("$(S)/orchard/writer/main.py", bytecode["inputs"])
                self.assertEqual(bytecode["cmds"][0]["cmd_args"][0], "$(B)/tools/py3cc/py3cc")
                self.assertEqual(binary["platform"], bytecode["platform"])
                self.assertNotIn("test-dependency:", stderr)


if __name__ == "__main__":
    unittest.main(verbosity=2)
