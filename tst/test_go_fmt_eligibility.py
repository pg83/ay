import unittest

import lib
from test_go_fmt_sources import fmt_files
from test_go_tests import AUTOCHECK, go_world, go_test_world, go_tool_args, between


class GoFmtEligibilityTest(unittest.TestCase):
    def test_missing_bare_sources_do_not_become_checks_or_disappear_from_builds(self):
        for spelling in ("absent.go", "worker/main.go"):
            with self.subTest(spelling=spelling):
                files = go_world()
                files["orchard/runner/ya.make"] = f"GO_PROGRAM()\nSRCS({spelling})\nEND()\n"
                # Package discovery metadata cannot resolve the declared file.
                files["orchard/runner/runner.go"] = "package runner\n"
                graph = lib.make(files, "orchard/runner", *AUTOCHECK)
                self.assertFalse(any(n["kv"].get("path", "").startswith("orchard/runner/")
                                     and n["kv"].get("path", "").endswith("/gofmt") for n in graph["graph"]))
                binary = lib.node_by_output(graph, "$(B)/orchard/runner/runner")
                self.assertIn("$(S)/orchard/runner/" + spelling,
                              between(go_tool_args(binary), "++srcs", "++asm-flags"))
                self.assertTrue(any(n["kv"].get("path") == "orchard/runner/govet" for n in graph["graph"]))

    def test_nested_existing_sources_and_rooted_missing_sources_keep_groups(self):
        files = go_world()
        files["orchard/runner/ya.make"] = (
            "GO_PROGRAM()\nSRCDIR(shared)\nCOPY_FILE(seed.txt generated.go)\n"
            "SRCS(worker/main.go worker/missing.go other.go "
            "${ARCADIA_ROOT}/orchard/runner/rooted/missing.go "
            "${CURDIR}/rooted/current.go generated.go)\nEND()\n")
        files["orchard/runner/worker/main.go"] = "package main\nfunc main() {}\n"
        files["orchard/runner/seed.txt"] = "package main\n"
        files["shared/other.go"] = "package main\n"
        graph = lib.make(files, "orchard/runner", *AUTOCHECK)
        self.assertEqual(fmt_files(graph, "orchard/runner/worker"), ["$(S)/orchard/runner/worker/main.go"])
        self.assertEqual(fmt_files(graph, "orchard/runner/rooted"), [
            "$(S)/orchard/runner/rooted/current.go", "$(S)/orchard/runner/rooted/missing.go"])
        self.assertEqual(fmt_files(graph, "shared"), ["$(S)/shared/other.go"])
        binary = lib.node_by_output(graph, "$(B)/orchard/runner/runner")
        generated = lib.node_by_output(graph, "$(B)/orchard/runner/generated.go")
        self.assertIn(generated["uid"], binary["deps"])
        self.assertIn("$(B)/orchard/runner/generated.go", binary["inputs"])

    def test_missing_test_declarations_keep_compiler_inputs_without_style_checks(self):
        files = go_test_world()
        files["orchard/check/ya.make"] = (
            "GO_TEST()\nSRCS(prod.go)\nGO_TEST_SRCS(alive_test.go missing_test.go)\n"
            "GO_XTEST_SRCS(external_test.go missing_external_test.go)\nEND()\n")
        for name in ("prod.go", "alive_test.go", "external_test.go"):
            files["orchard/check/" + name] = "package check\n"
        graph = lib.make(files, "orchard/check", *AUTOCHECK)
        self.assertEqual(fmt_files(graph, "orchard/check"), [
            "$(S)/orchard/check/alive_test.go", "$(S)/orchard/check/external_test.go",
            "$(S)/orchard/check/prod.go"])
        binary = lib.node_by_output(graph, "$(B)/orchard/check/check")
        args = go_tool_args(binary)
        self.assertIn("$(S)/orchard/check/missing_test.go", between(args, "++test_srcs", "++xtest_srcs"))
        self.assertIn("$(S)/orchard/check/missing_external_test.go", args[args.index("++xtest_srcs") + 1:])


if __name__ == "__main__":
    unittest.main(verbosity=2)
