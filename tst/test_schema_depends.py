import unittest

import lib
from test_proto_targets import target_files
from test_proto_py2_context import proto_world
import test_nots


def workspace(selector=""):
    files = target_files()
    files.update(proto_world())
    files["p/ya.make"] = f"PROTO_SCHEMA(catalog)\n{lib.BARE_MODULE}{selector}SRCS(a.proto)\nEND()\n"
    files["p/a.proto"] = 'syntax = "proto2";\nmessage A {}\n'
    files["tests/ya.make"] = "EXECTEST()\nRUN(true)\nDEPENDS(p)\nEND()\n"
    return files


class SchemaDependsTest(unittest.TestCase):
    def test_default_depends_includes_java_go_and_descriptor_modules(self):
        graph = lib.make(workspace(), "tests", "-t", "--strip-packages-from-results")
        suite = lib.node_by_output(graph, "$(B)/tests/test-results/exectest/meta.json")
        for output in ("libcatalog.a", "catalog.jar", "libpycatalog.global.a", "libpy3catalog.global.a",
                       "catalog.a", "catalog.self.protodesc", "catalog.protodesc"):
            with self.subTest(output=output):
                node = lib.node_by_output(graph, "$(B)/p/" + output)
                self.assertIn(node["uid"], suite["deps"])
        self.assertFalse(any("$(B)/p/output.tar" in n["outputs"] for n in graph["graph"]))

    def test_typescript_schema_contexts_are_opt_in(self):
        files = workspace("INCLUDE_TAGS(TS_PROTO_FROM_SCHEMA TS_PREPARE_DEPS)\nEXCLUDE_TAGS(CPP_PROTO_FROM_SCHEMA JAVA_PROTO_FROM_SCHEMA PY_PROTO_FROM_SCHEMA PY3_PROTO_FROM_SCHEMA GO_PROTO_FROM_SCHEMA)\n")
        files.update(test_nots.NotsTests().project())
        deps = "library/typescript/ts-proto-deps"
        files[f"{deps}/ya.make"] = "TS_PACKAGE()\nEND()\n"
        files[f"{deps}/package.json"] = '{}'
        files[f"{deps}/pnpm-lock.yaml"] = "lockfileVersion: '9.0'\npackages: {}\n"
        graph = lib.make(files, "tests", "-t", "-DAUTOCHECK=yes", "--strip-packages-from-results")
        suite = lib.node_by_output(graph, "$(B)/tests/test-results/exectest/meta.json")
        for output in ("package.json", "output.tar"):
            node = lib.node_by_output(graph, "$(B)/p/" + output)
            self.assertIn(node["uid"], suite["deps"])

    def test_depends_selects_schema_language_contexts_despite_start_target(self):
        files = workspace("EXCLUDE_TAGS(GO_PROTO_FROM_SCHEMA JAVA_PROTO_FROM_SCHEMA)\n")
        graph = lib.make(files, "tests", "-t", "--strip-packages-from-results")
        suite = lib.node_by_output(graph, "$(B)/tests/test-results/exectest/meta.json")
        for name in ("libcatalog.a", "libpycatalog.global.a", "libpy3catalog.global.a", "catalog.protodesc"):
            node = lib.node_by_output(graph, "$(B)/p/" + name)
            self.assertIn(node["uid"], suite["deps"])
        outputs = {out for node in graph["graph"] for out in node["outputs"]}
        self.assertFalse("$(B)/p/catalog.a" in outputs, "unexpected Go schema result")
        self.assertFalse("$(B)/p/catalog.jar" in outputs, "unexpected Java schema result")

    def test_schema_explicit_root_remains_descriptor_only(self):
        graph = lib.make(workspace(), "p", "--strip-packages-from-results")
        lib.node_by_output(graph, "$(B)/p/catalog.protodesc")
        outputs = {out for node in graph["graph"] for out in node["outputs"]}
        self.assertNotIn("$(B)/p/libcatalog.a", outputs)
        self.assertNotIn("$(B)/p/libpy3catalog.global.a", outputs)

    def test_disabled_start_context_does_not_replace_an_excluded_final_root(self):
        graph = lib.make(workspace("ONLY_TAGS(CPP_PROTO_FROM_SCHEMA)\n"), "p", "--strip-packages-from-results")
        self.assertEqual(graph["result"], [])
        self.assertFalse(any("$(B)/p/libcatalog.a" in n["outputs"] for n in graph["graph"]))

    def test_only_tags_selects_the_named_schema_context(self):
        for tag, output in (("CPP_PROTO_FROM_SCHEMA", "libcatalog.a"),
                            ("PY3_PROTO_FROM_SCHEMA", "libpy3catalog.global.a")):
            with self.subTest(tag=tag):
                graph = lib.make(workspace(f"ONLY_TAGS({tag})\n"), "tests", "-t", "--strip-packages-from-results")
                node = lib.node_by_output(graph, "$(B)/p/" + output)
                suite = lib.node_by_output(graph, "$(B)/tests/test-results/exectest/meta.json")
                self.assertIn(node["uid"], suite["deps"])

    def test_depends_first_schema_context_refines_selection_once(self):
        files = workspace('IF(MODULE_TAG == "CPP_PROTO_FROM_SCHEMA")\nEXCLUDE_TAGS(JAVA_PROTO_FROM_SCHEMA GO_PROTO_FROM_SCHEMA PY_PROTO_FROM_SCHEMA)\nENDIF()\n')
        graph = lib.make(files, "tests", "-t", "--strip-packages-from-results")
        lib.node_by_output(graph, "$(B)/p/libpy3catalog.global.a")
        outputs = {out for node in graph["graph"] for out in node["outputs"]}
        self.assertFalse("$(B)/p/catalog.a" in outputs, "first schema context must exclude Go")
        self.assertFalse("$(B)/p/catalog.jar" in outputs, "first schema context must exclude Java")
        self.assertFalse("$(B)/p/libpycatalog.global.a" in outputs, "first schema context must exclude Python 2")


if __name__ == "__main__":
    unittest.main()
