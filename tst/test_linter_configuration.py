import unittest

import lib
from test_go_tests import go_module, go_world, go_test_world, go_proto_world, test_node, test_paths


LINT = """
_ADD_CUSTOM_EXPLICIT_LINTER_CHECK(NAME policy WRAPPER_SCRIPT tools/check.py
    FILES checked.txt DEPENDS checker)
"""


def add_checker(files):
    lib.tool_program(files, "checker", "checker")
    files["tools/check.py"] = ""


class LinterConfigurationTest(unittest.TestCase):
    def test_uncollected_peer_linter_configures_dependencies_with_normal_retention(self):
        for language in ("native", "go"):
            for add_modules in (False, True):
                for strip in (False, True):
                    with self.subTest(language=language, add_modules=add_modules, strip=strip):
                        files = go_world()
                        add_checker(files)
                        if language == "go":
                            go_module(files, "root", macros="PEERDIR(peer)\n")
                            go_module(files, "peer", macros=LINT)
                        else:
                            files["root/ya.make"] = f"PROGRAM()\n{lib.BARE_MODULE}PEERDIR(peer)\nEND()\n"
                            files["peer/ya.make"] = f"LIBRARY()\n{lib.BARE_MODULE}{LINT}END()\n"
                        files["peer/checked.txt"] = ""
                        flags = ["--add-modules-to-results"] if add_modules else []
                        if strip:
                            flags.append("--strip-packages-from-results")
                        graph = lib.make(files, "root", "-t", *flags)
                        self.assertNotIn("peer/policy", test_paths(graph))
                        nodes = [n for n in graph["graph"] if "$(B)/checker/checker" in n["outputs"]]
                        if strip:
                            self.assertEqual(nodes, [])
                        else:
                            self.assertEqual(len(nodes), 1)
                            self.assertEqual(nodes[0]["uid"] in graph["result"], add_modules)

    def test_linter_dependencies_do_not_pollute_ordinary_runtime_suites(self):
        for language in ("native", "go"):
            with self.subTest(language=language):
                files = go_test_world() if language == "go" else {}
                add_checker(files)
                if language == "go":
                    files["root/ya.make"] = f"GO_TEST()\nGO_TEST_SRCS(check_test.go)\n{LINT}END()\n"
                    files["root/check_test.go"] = "package root\n"
                    runtime = "root/root"
                else:
                    files["root/ya.make"] = f"UNITTEST()\n{lib.BARE_MODULE}{LINT}SRCS(test.cpp)\nEND()\n"
                    files["root/test.cpp"] = ""
                    files["library/cpp/testing/unittest_main/ya.make"] = f"LIBRARY()\n{lib.BARE_MODULE}END()\n"
                    runtime = "root/unittest"
                files["root/checked.txt"] = ""
                graph = lib.make(files, "root", "-t", "--strip-packages-from-results")
                checker = lib.node_by_output(graph, "$(B)/checker/checker")
                self.assertIn(checker["uid"], graph["result"])
                self.assertIn(checker["uid"], test_node(graph, "root/policy")["deps"])
                self.assertNotIn(checker["uid"], test_node(graph, runtime)["deps"])

    def test_size_filtered_root_linter_still_configures_its_dependency(self):
        files = go_world()
        add_checker(files)
        go_module(files, "root", macros=LINT)
        files["root/checked.txt"] = ""
        graph = lib.make(files, "root", "-t", "--test-size", "large", "--strip-packages-from-results")
        checker = lib.node_by_output(graph, "$(B)/checker/checker")
        self.assertIn(checker["uid"], graph["result"])
        self.assertEqual(test_paths(graph), [])

    def test_enabled_configuration_traversal_does_not_require_test_collection(self):
        files = go_world()
        add_checker(files)
        go_module(files, "root", macros=LINT)
        files["root/checked.txt"] = ""
        graph = lib.make(files, "root", "-DTRAVERSE_DEPENDS=yes", "--strip-packages-from-results")
        checker = lib.node_by_output(graph, "$(B)/checker/checker")
        self.assertIn(checker["uid"], graph["result"])
        self.assertEqual(test_paths(graph), [])

    def test_skipped_linter_does_not_record_configuration_dependencies(self):
        for prefix, lint in (("NO_LINT()\n", LINT), ("SET(TIDY yes)\n", LINT),
                             ("", LINT.replace("FILES checked.txt", "FILES"))):
            with self.subTest(prefix=prefix, lint=lint):
                files = go_world()
                add_checker(files)
                go_module(files, "root", macros=prefix + lint)
                files["root/checked.txt"] = ""
                graph = lib.make(files, "root", "-t", "--add-modules-to-results")
                self.assertFalse(any("$(B)/checker/checker" in n["outputs"] for n in graph["graph"]))
                self.assertNotIn("root/policy", test_paths(graph))

    def test_excluded_context_selection_probe_has_no_configuration_side_effects(self):
        files = go_proto_world()
        files.update({
            "tests/ya.make": "EXECTEST()\nRUN(true)\nDEPENDS(p)\nEND()\n",
            "p/ya.make": (
                "PROTO_LIBRARY()\nONLY_TAGS(GO_PROTO)\nSRCS(p.proto)\n"
                'IF(MODULE_TAG == "CPP_PROTO")\n' + LINT + "ENDIF()\nEND()\n"
            ),
            "p/p.proto": 'syntax = "proto3";\n',
            "p/checked.txt": "",
            "tools/check.py": "",
            "checker/ya.make": f"PROGRAM()\n{lib.BARE_MODULE}PEERDIR(missing-peer)\nEND()\n",
        })
        graph = lib.make(files, "tests", "-t", "--add-modules-to-results")
        self.assertFalse(any("$(B)/checker/checker" in n["outputs"] for n in graph["graph"]))
        self.assertNotIn("p/policy", test_paths(graph))


if __name__ == "__main__":
    unittest.main()
