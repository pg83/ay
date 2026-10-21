import unittest

import lib
from test_python2 import STUB_LIBRARY, python2_base
from test_python_suites import fixture as python3_fixture


def validation_nodes(graph, project, name):
    output = f"$(B)/{project}/test-results/{name}/meta.json"
    return [node for node in graph["graph"] if output in node["outputs"]]


class ValidationVariantsTest(unittest.TestCase):
    def test_split_python_program_retains_import_and_source_checks(self):
        files = python3_fixture("SET(VALIDATE_DATA yes)\nDATA(sbr://42)\n", macro="PY3_PROGRAM")
        graph = lib.make(files, "lib", "-t", "--sandboxing")
        program = lib.node_by_output(graph, "$(B)/lib/lib")
        imports = lib.node_by_output(graph, "$(B)/lib/test-results/import_test/meta.json")
        self.assertIn(program["uid"], imports["deps"])
        style = lib.node_by_output(graph, "$(B)/lib/test-results/flake8/meta.json")
        self.assertIn("$(S)/lib/a.py", style["inputs"])
        nodes = validation_nodes(graph, "lib", "validate_data_sbr")
        self.assertEqual(len(nodes), 1)
        self.assertEqual(nodes[0]["inputs"], ["$(S)/lib", "42"])

    def test_library_checks_merge_the_configured_peer_variant(self):
        files = python2_base()
        for peer in ("library/python/pytest", "library/python/runtime_py3/main", "contrib/tools/python3/Modules/_sqlite"):
            files[f"{peer}/ya.make"] = STUB_LIBRARY
            files[f"{peer}/s.cpp"] = "int s;\n"
        for tool in ("tools/py3cc", "tools/py3cc/slow"):
            lib.tool_program(files, tool, tool.rsplit("/", 1)[-1])
        files["mod/ya.make"] = (
            "PY23_LIBRARY()\nNO_PYTHON_INCLUDES()\nPY_SRCS(mod.py)\nSET(VALIDATE_DATA yes)\n"
            "FROM_SANDBOX(FILE 30 OUT_NOAUTO shared.bin)\n"
            "IF(PYTHON2)\nFROM_SANDBOX(FILE 20 OUT_NOAUTO variant.bin)\n"
            "ELSE()\nFROM_SANDBOX(FILE 10 OUT_NOAUTO variant.bin)\nENDIF()\n"
            "END()\nRECURSE_FOR_TESTS(tests)\n"
        )
        files["mod/mod.py"] = ""
        files["mod/tests/ya.make"] = "PY23_TEST()\nNO_PYTHON_INCLUDES()\nPEERDIR(mod)\nTEST_SRCS(test_mod.py)\nEND()\n"
        files["mod/tests/test_mod.py"] = ""
        for targets in (("mod",), ("mod/tests", "mod")):
            with self.subTest(targets=targets):
                graph = lib.make(files, *targets, "-t", "--sandboxing", opensource=False)
                nodes = validation_nodes(graph, "mod", "validate_resource")
                self.assertEqual(len(nodes), 1)
                self.assertEqual(nodes[0]["inputs"], ["$(S)/mod", "10", "20", "30"])
                for suite in ("flake8", "py2_flake8"):
                    lib.node_by_output(graph, f"$(B)/mod/test-results/{suite}/meta.json")
                for binary, suite in (("py2_mod-tests", "pytest"), ("mod-tests", "py3test")):
                    program = lib.node_by_output(graph, "$(B)/mod/tests/" + binary)
                    run = lib.node_by_output(graph, f"$(B)/mod/tests/test-results/{suite}/meta.json")
                    self.assertIn(program["uid"], run["deps"])

    def test_library_without_a_configured_peer_keeps_its_check(self):
        files = python2_base()
        files["mod/ya.make"] = (
            "PY23_LIBRARY()\nNO_PYTHON_INCLUDES()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
            "SET(VALIDATE_DATA yes)\nFROM_SANDBOX(FILE 42 OUT_NOAUTO resource.bin)\nEND()\n"
        )
        graph = lib.make(files, "mod", "-t", "--sandboxing", opensource=False)
        nodes = validation_nodes(graph, "mod", "validate_resource")
        self.assertEqual(len(nodes), 1)
        self.assertEqual(nodes[0]["inputs"], ["$(S)/mod", "42"])

    def test_multimodule_checks_merge_resource_ids_once(self):
        files = python2_base()
        for peer in ("library/python/pytest", "library/python/runtime_py3/main", "contrib/tools/python3/Modules/_sqlite"):
            files[f"{peer}/ya.make"] = STUB_LIBRARY
            files[f"{peer}/s.cpp"] = "int s;\n"
        for tool in ("tools/py3cc", "tools/py3cc/slow"):
            lib.tool_program(files, tool, tool.rsplit("/", 1)[-1])
        files["mod/ya.make"] = (
            "PY23_TEST()\nNO_PYTHON_INCLUDES()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
            "TEST_SRCS(test_mod.py)\n"
            "SET(VALIDATE_DATA yes)\n"
            "FROM_SANDBOX(FILE 30 OUT_NOAUTO shared.bin)\nDATA(sbr://30)\n"
            "IF(PYTHON2)\n"
            "FROM_SANDBOX(FILE 20 OUT_NOAUTO variant.bin)\nDATA(sbr://20)\n"
            "ELSE()\n"
            "FROM_SANDBOX(FILE 10 OUT_NOAUTO variant.bin)\nDATA(sbr://10)\n"
            "ENDIF()\nEND()\n"
        )
        files["mod/test_mod.py"] = ""
        graph = lib.make(files, "mod", "--target-platform-tests", "--sandboxing", opensource=False)
        for name in ("validate_resource", "validate_data_sbr"):
            with self.subTest(name=name):
                nodes = validation_nodes(graph, "mod", name)
                self.assertEqual(len(nodes), 1)
                self.assertEqual(nodes[0]["inputs"], ["$(S)/mod", "10", "20", "30"])
                args = nodes[0]["cmds"][-1]["cmd_args"]
                for resource_id in ("10", "20", "30"):
                    self.assertEqual(args.count(resource_id), 1)

    def test_checks_keep_distinct_projects_and_types(self):
        files = {}
        for project in ("first", "second"):
            files[f"{project}/ya.make"] = (
                "GEN_LIBRARY()\nSET(VALIDATE_DATA yes)\n"
                "FROM_SANDBOX(FILE 42 OUT_NOAUTO resource.bin)\n"
                "DATA(sbr://42)\nEND()\n"
            )
        graph = lib.make(files, "first", "second", "--target-platform-tests", "--sandboxing")
        for project in ("first", "second"):
            for name in ("validate_resource", "validate_data_sbr"):
                with self.subTest(project=project, name=name):
                    nodes = validation_nodes(graph, project, name)
                    self.assertEqual(len(nodes), 1)
                    self.assertEqual(nodes[0]["inputs"], [f"$(S)/{project}", "42"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
