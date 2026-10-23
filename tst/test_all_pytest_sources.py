"""ALL_PYTEST_SRCS forwards lexical glob results through TEST_SRCS."""
import unittest

import lib
from test_all_python_forwarding import complete_nodes
from test_python2 import python2_base
from test_python_suites import fixture as style_fixture
from test_python_srcs import python_base


SOURCES = ("helper.py", "root_test.py", "test_alpha.py", "test_both_test.py", "zeta.py", "left/helper.py", "left/l_test.py", "left/test_l.py", "left/deep/test_d.py", "right/helper.py", "right/test_r.py")


def fixture(body, kind="PY3TEST"):
    files = python2_base() if kind == "PY2_LIBRARY" else python_base()
    files["maple/ya.make"] = f"{kind}()\n{lib.BARE_MODULE}NO_CHECK_IMPORTS()\n{body}\nEND()\n"
    files["library/python/pytest/ya.make"] = f"LIBRARY()\n{lib.BARE_MODULE}END()\n"
    for src in SOURCES:
        files["maple/" + src] = "value = 1\n"
    files["maple/ignored.txt"] = ""
    return files


class AllPytestSourcesTest(unittest.TestCase):
    def paired(self, options, explicit, kind="PY3TEST"):
        macro = lib.make(fixture("ALL_PYTEST_SRCS(" + options + ")", kind), "maple", "-t")
        direct = lib.make(fixture("TEST_SRCS(" + " ".join(explicit) + ")", kind), "maple", "-t")
        self.assertEqual(complete_nodes(macro), complete_nodes(direct))
        self.assertEqual(macro["result"], direct["result"])
        return macro

    def test_default_and_scope_arguments_keep_test_namespace(self):
        expected = list(SOURCES[:5])
        for option in ("", "TOP_LEVEL NAMESPACE ignored"):
            with self.subTest(option=option):
                self.paired(option, expected)

    def test_patterns_directories_and_recursive_order(self):
        for options, expected in (
            ("ONLY_TEST_FILES", ["test_alpha.py", "test_both_test.py", "root_test.py"]),
            ("right left right", ["right/helper.py", "right/test_r.py", "left/helper.py", "left/l_test.py", "left/test_l.py"]),
            ("RECURSIVE", ["left/helper.py", "left/l_test.py", "left/test_l.py", "right/helper.py", "right/test_r.py", "left/deep/test_d.py", *SOURCES[:5]]),
            ("RECURSIVE ONLY_TEST_FILES right left", ["right/test_r.py", "left/deep/test_d.py", "left/test_l.py", "left/l_test.py"]),
            ("missing", []),
        ):
            with self.subTest(options=options):
                self.paired(options, expected)

    def test_python2_uses_existing_test_source_resources(self):
        self.paired("", list(SOURCES[:5]), kind="PY2_LIBRARY")

    def test_style_suites_receive_discovered_test_sources(self):
        files = style_fixture("STYLE_PYTHON()\nSTYLE_RUFF()\n")
        files["lib/ya.make"] = files["lib/ya.make"].replace("PY_SRCS(a.py)", "ALL_PYTEST_SRCS()")
        all_graph = lib.make(files, "lib", "-t")
        files["lib/ya.make"] = files["lib/ya.make"].replace("ALL_PYTEST_SRCS()", "TEST_SRCS(a.py)")
        direct = lib.make(files, "lib", "-t")
        self.assertEqual(complete_nodes(all_graph), complete_nodes(direct))
        self.assertEqual(all_graph["result"], direct["result"])

    def test_physical_generated_collision_uses_existing_test_source_selection(self):
        files = fixture("RUN_PROGRAM(tools/seed OUT_NOAUTO helper.py)\nALL_PYTEST_SRCS()")
        lib.tool_program(files, "tools/seed", "seed")
        actual = lib.make(files, "maple", "-t")
        helper = lib.node_by_output(actual, "$(B)/maple/helper.py.yapyc3")
        self.assertIn("$(S)/maple/helper.py", helper["inputs"])
        self.assertFalse(any("$(B)/maple/helper.py" in n["outputs"] for n in actual["graph"]))
        files["maple/ya.make"] = files["maple/ya.make"].replace("ALL_PYTEST_SRCS()", "TEST_SRCS(_MR " + " ".join("${ARCADIA_ROOT}/maple/" + src for src in SOURCES[:5]) + ")")
        expected = lib.make(files, "maple", "-t")
        self.assertEqual(complete_nodes(actual), complete_nodes(expected))
        self.assertEqual(actual["result"], expected["result"])

    def test_namespace_requires_its_native_argument(self):
        result = lib.make_process(fixture("ALL_PYTEST_SRCS(NAMESPACE)"), "maple")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("NAMESPACE", result.stderr)

    def test_recursive_glob_rejects_nested_module(self):
        files = fixture("ALL_PYTEST_SRCS(RECURSIVE)")
        files["maple/left/ya.make"] = "LIBRARY()\nEND()\n"
        result = lib.make_process(files, "maple")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("ya.make", result.stderr)

    def test_literal_module_relative_marker_does_not_weaken_raw_validation(self):
        files = fixture("TEST_SRCS(_MR __main__.py)")
        files["maple/__main__.py"] = ""
        result = lib.make_process(files, "maple")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("__main__.py", result.stderr)

    def test_glob_main_keeps_native_internal_test_namespace(self):
        files = fixture("ALL_PYTEST_SRCS()")
        files["maple/__main__.py"] = ""
        graph = lib.make(files, "maple")
        lib.node_by_output(graph, "$(B)/maple/__main__.py.yapyc3")
        self.assertTrue(any("resfs/file/py/__tests__/__main__.py" in arg for n in graph["graph"] for c in n["cmds"] for arg in c["cmd_args"]))

    def test_shared_include_keeps_directory_facts_in_each_module_context(self):
        files = fixture("")
        files["shared/tests.inc"] = "ALL_PYTEST_SRCS(${TEST_DIR})\n"
        for module, directory in (("copper", "alpha"), ("silver", "beta")):
            files[module + "/ya.make"] = f"PY3_LIBRARY()\n{lib.BARE_MODULE}SET(TEST_DIR {directory})\nINCLUDE(${{ARCADIA_ROOT}}/shared/tests.inc)\nEND()\n"
            files[f"{module}/{directory}/test_leaf.py"] = ""
        for targets in (("copper", "silver"), ("silver", "copper")):
            with self.subTest(targets=targets):
                actual = lib.make(files, *targets)
                explicit = dict(files)
                for module, directory in (("copper", "alpha"), ("silver", "beta")):
                    explicit[module + "/ya.make"] = explicit[module + "/ya.make"].replace("INCLUDE(${ARCADIA_ROOT}/shared/tests.inc)", f"TEST_SRCS({directory}/test_leaf.py)")
                expected = lib.make(explicit, *targets)
                self.assertEqual(complete_nodes(actual), complete_nodes(expected))
                self.assertEqual(actual["result"], expected["result"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
