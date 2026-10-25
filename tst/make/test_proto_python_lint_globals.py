"""Python proto variants inherit the native internal no-lint default."""
import unittest

from tst import lib
from tst.make.test_proto_py2_context import proto_world
from tst.make.test_python_suites import fixture as python_fixture
from tst.make.test_test_macro_values import option_values


def fixture(mode="default", python2=False, schema=False, sources="seed.proto"):
    files = proto_world()
    files.update(python_fixture("NO_PYTHON_INCLUDES()\nNO_IMPORT_TRACING()\n"
                                "DISABLE(PYTHON_SQLITE3)\nALLOCATOR(FAKE)\n"
                                "PY_MAIN(a:main)\nPEERDIR(grove/branch)\n",
                                macro="PY2_PROGRAM" if python2 else "PY3_PROGRAM"))
    files["lib/a.py"] = "def main(): pass\n"
    files["lint/flake8.conf"] = ""
    files["lint/ruff.toml"] = ""
    files["build/config/tests/py_style/default_configs.json"] = '{"flake8":"lint/flake8.conf","py2_flake8":"lint/flake8.conf","ruff":"lint/ruff.toml"}'
    files["build/external_resources/flake8_py2/ya.make"] = lib.resource_lib("FLAKE8_PY2", 124)
    prologue = "SET(_NO_LINT_VALUE)\n" if mode == "pre-header" else ""
    setting = "SET(_NO_LINT_VALUE)\n" if mode == "enabled" else ""
    if mode == "explicit-disabled":
        setting = "SET(_NO_LINT_VALUE none_internal)\n"
    module = "PY23_LIBRARY" if mode == "ordinary" else "PROTO_SCHEMA" if schema else "PROTO_LIBRARY"
    files["grove/branch/ya.make"] = (prologue + module + "()\n" + lib.BARE_MODULE
                                      + "NO_MYPY()\n" + setting + "SRCS(" + sources + ")\nEND()\n")
    if mode == "ordinary":
        files["grove/branch/ya.make"] = files["grove/branch/ya.make"].replace("SRCS(", "PY_SRCS(")
    files["grove/branch/seed.proto"] = 'syntax="proto2"; message Seed {}\n'
    files["grove/branch/seed.py"] = ""
    files["build/conf/autoincludes.json"] = '["grove"]'
    files["grove/linters.make.inc"] = "IF (MODULE_LANG == PY3 OR MODULE_LANG == PY2)\nSTYLE_RUFF()\nENDIF()\n"
    return files, "lib", []


def import_resources(graph):
    node = lib.node_by_output(graph, "$(B)/lib/test-results/import_test/meta.json")
    return option_values(node, "--global-resource")


class ProtoPythonLintGlobalsTest(unittest.TestCase):
    def test_proto_defaults_and_pre_header_values_do_not_publish_lint_peers(self):
        for python2 in (False, True):
            for schema in (False, True):
                for mode in ("default", "pre-header", "explicit-disabled"):
                    with self.subTest(python2=python2, schema=schema, mode=mode):
                        files, target, flags = fixture(mode, python2, schema)
                        graph = lib.make(files, target, "-t", "--sandboxing", *flags)
                        self.assertFalse(any(v.startswith("RUFF_RESOURCE_GLOBAL::") for v in import_resources(graph)))
                        suffix = "__intpy2___pb2.py" if python2 else "__intpy3___pb2.py"
                        producer = lib.node_by_output(graph, "$(B)/grove/branch/seed" + suffix)
                        self.assertIn("$(S)/grove/branch/seed.proto", producer["inputs"])

    def test_explicit_late_override_still_publishes_the_real_resource(self):
        for python2 in (False, True):
            for schema in (False, True):
                with self.subTest(python2=python2, schema=schema):
                    files, target, flags = fixture("enabled", python2, schema)
                    graph = lib.make(files, target, "-t", "--sandboxing", *flags)
                    self.assertIn("RUFF_RESOURCE_GLOBAL::$(B)/resources/RUFF", import_resources(graph))

    def test_empty_proto_default_and_ordinary_python_library_remain_distinct(self):
        for python2 in (False, True):
            for mode, sources, expected in (("default", "", False), ("ordinary", "seed.py", True)):
                with self.subTest(python2=python2, mode=mode):
                    files, target, flags = fixture(mode, python2, sources=sources)
                    graph = lib.make(files, target, "-t", "--sandboxing", *flags)
                    self.assertEqual(any(v.startswith("RUFF_RESOURCE_GLOBAL::") for v in import_resources(graph)), expected)


if __name__ == "__main__":
    unittest.main(verbosity=2)
