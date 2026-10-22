import unittest

import lib
from test_java import contrib


VENDOR = "contrib/java/org/renamed"
OLD = f"{VENDOR}/wire/4/wire-4.jar"
SELECTED = f"{VENDOR}/wire/9/wire-9.jar"


def workspace():
    return lib.java_tree(**{
        "checks/ya.make": "EXECTEST()\nRUN(true)\nDEPENDS(schema)\nEND()\n",
        "schema/ya.make": (
            "PROTO_LIBRARY()\nONLY_TAGS(JAVA_PROTO)\n"
            f"PEERDIR({VENDOR}/facade/1)\n"
            f"IF(JAVA_PROTO)\nDEPENDENCY_MANAGEMENT({VENDOR}/wire/9)\nENDIF()\nEND()\n"
        ),
        **contrib(f"{VENDOR}/facade/1", f"{VENDOR}/wire/4", jar=101, src=201),
        **contrib(f"{VENDOR}/wire/4", jar=102, src=202),
        **contrib(f"{VENDOR}/wire/9", jar=103, src=203),
        **contrib("contrib/java/com/google/protobuf/protobuf-java/4.33.0", jar=104, src=204),
    })


class JavaDependsResultRootsTest(unittest.TestCase):
    def test_nonfinal_multimodule_root_keeps_only_consumed_version_producers(self):
        for options in ([], ["--add-modules-to-results"]):
            with self.subTest(options=options):
                graph = lib.make(workspace(), "checks", "-t", *options)
                jar = lib.node_by_output(graph, "$(B)/schema/schema.jar")
                selected = lib.node_by_output(graph, "$(B)/" + SELECTED)
                self.assertIn(selected["uid"], jar["deps"])
                self.assertIn("$(B)/" + SELECTED, jar["inputs"])
                self.assertIn(jar["uid"], graph["result"])
                self.assertFalse(any("$(B)/" + OLD in n["outputs"] for n in graph["graph"]))

    def test_explicit_old_version_root_remains_a_result(self):
        for targets in (("checks", f"{VENDOR}/wire/4"), (f"{VENDOR}/wire/4", "checks")):
            with self.subTest(targets=targets):
                graph = lib.make(workspace(), *targets, "-t", "--add-modules-to-results")
                old = lib.node_by_output(graph, "$(B)/" + OLD)
                self.assertIn(old["uid"], graph["result"])
                jar = lib.node_by_output(graph, "$(B)/schema/schema.jar")
                self.assertNotIn(old["uid"], jar["deps"])

    def test_final_java_dependency_still_expands_configured_bundle_peers(self):
        for nested in (False, True):
            with self.subTest(nested=nested):
                files = workspace()
                peer = "schema" if nested else f"{VENDOR}/facade/1"
                files["app/ya.make"] = (
                    f"JAVA_LIBRARY_NON_SPLIT()\nPEERDIR({peer})\n"
                    f"DEPENDENCY_MANAGEMENT({VENDOR}/wire/9)\nEND()\n"
                )
                files["checks/ya.make"] = "EXECTEST()\nRUN(true)\nDEPENDS(app)\nEND()\n"
                graph = lib.make(files, "checks", "-t", "--add-modules-to-results")
                old = lib.node_by_output(graph, "$(B)/" + OLD)
                self.assertIn(old["uid"], graph["result"])


if __name__ == "__main__":
    unittest.main()
