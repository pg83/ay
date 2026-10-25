"""A copied SC input keeps the resolved producer identity of its origin."""
import unittest

from tst import lib


MODULE = "grove/packs"
WRITER = "tools/seed_writer"
SPLITTER = "tools/part_maker"


def fixture(body, files=None):
    result = {MODULE + "/ya.make": f"LIBRARY()\n{lib.BARE_MODULE}{body}END()\n"}
    result.update(files or {})
    lib.tool_program(result, WRITER, "renamed-seed-writer")
    lib.tool_program(result, SPLITTER, "renamed-part-maker")
    return result


def split(prefix="parts"):
    return f"SPLIT_CODEGEN({SPLITTER} {prefix} NGrove OUT_NUM 6)\n"


class SplitCodegenInputHandoffTest(unittest.TestCase):
    def test_unrooted_copy_origin_waits_for_the_declared_noauto_producer(self):
        statements = [
            split(),
            "COPY_FILE(origin.in parts.in)\n",
            f"RUN_PROGRAM({WRITER} IN ${{CURDIR}}/seed.dat OUT_NOAUTO receipt.json origin.in)\n",
        ]
        for order in ((0, 1, 2), (2, 0, 1), (1, 2, 0)):
            with self.subTest(order=order):
                graph = lib.make(fixture("".join(statements[i] for i in order),
                                         {MODULE + "/seed.dat": "physical seed\n"}), MODULE)
                copied = lib.node_by_output(graph, f"$(B)/{MODULE}/parts.in")
                self.assertEqual(copied["cmds"][0]["cmd_args"][-2:],
                                 [f"$(B)/{MODULE}/origin.in", f"$(B)/{MODULE}/parts.in"])
                original = lib.node_by_output(graph, f"$(B)/{MODULE}/origin.in")
                writer = lib.node_by_output(graph, f"$(B)/{WRITER}/renamed-seed-writer")
                splitter = lib.node_by_output(graph, f"$(B)/{SPLITTER}/renamed-part-maker")
                self.assertEqual(original["outputs"],
                                 [f"$(B)/{MODULE}/receipt.json", f"$(B)/{MODULE}/origin.in"])
                self.assertIn(writer["uid"], original["deps"])
                self.assertIn(writer["outputs"][0], original["inputs"])
                self.assertIn(f"$(S)/{MODULE}/seed.dat", original["inputs"])
                self.assertIn(original["uid"], copied["deps"])
                self.assertIn(original["outputs"][0], copied["inputs"])
                self.assertIn(f"$(B)/{MODULE}/origin.in", copied["inputs"])
                produced = lib.node_by_output(graph, f"$(B)/{MODULE}/parts.0.cpp")
                self.assertEqual(produced["cmds"][0]["cmd_args"], [
                    splitter["outputs"][0], f"$(B)/{MODULE}/parts.in", f"$(B)/{MODULE}/parts.cpp",
                    f"$(B)/{MODULE}/parts.h", "--cpp-parts", "1", "NGrove",
                ])
                for dependency in (copied, original, splitter):
                    self.assertIn(dependency["uid"], produced["deps"])
                for path in ("parts.in", "origin.in", "receipt.json"):
                    self.assertIn(f"$(B)/{MODULE}/{path}", produced["inputs"])
                self.assertNotIn(f"$(S)/{MODULE}/origin.in", produced["inputs"])
                self.assertNotIn(f"$(S)/{MODULE}/seed.dat", produced["inputs"])
                for output in ("parts.0.cpp.o", "parts.5.cpp.o"):
                    compiled = lib.node_by_output(graph, f"$(B)/{MODULE}/{output}")
                    self.assertIn(produced["uid"], compiled["deps"])
                    self.assertIn(f"$(B)/{MODULE}/parts.0.cpp", compiled["inputs"])
                    self.assertNotIn(f"$(B)/{MODULE}/origin.in", compiled["inputs"])

    def test_physical_copy_origin_wins_over_a_same_name_producer(self):
        graph = lib.make(fixture(
            split() + "COPY_FILE(origin.in parts.in)\n" +
            f"RUN_PROGRAM({WRITER} IN ${{BINDIR}}/parts.h OUT_NOAUTO origin.in)\n",
            {MODULE + "/origin.in": "physical source\n"},
        ), MODULE)
        copied = lib.node_by_output(graph, f"$(B)/{MODULE}/parts.in")
        produced = lib.node_by_output(graph, f"$(B)/{MODULE}/parts.0.cpp")
        self.assertEqual(copied["cmds"][0]["cmd_args"][-2:],
                         [f"$(S)/{MODULE}/origin.in", f"$(B)/{MODULE}/parts.in"])
        self.assertIn(copied["uid"], produced["deps"])
        self.assertIn(f"$(S)/{MODULE}/origin.in", produced["inputs"])
        self.assertNotIn(f"$(B)/{MODULE}/origin.in", produced["inputs"])
        self.assertFalse(any(f"$(B)/{MODULE}/origin.in" in node["outputs"] for node in graph["graph"]))

    def test_source_search_for_copy_origin_is_preserved(self):
        for prefix in ("pool/templates", ""):
            with self.subTest(prefix=prefix):
                graph = lib.make(fixture(
                    "SRCDIR(pool/templates)\n" + split() + "COPY_FILE(origin.in parts.in)\n" +
                    f"RUN_PROGRAM({WRITER} IN ${{BINDIR}}/parts.h OUT_NOAUTO origin.in)\n",
                    {(prefix + "/" if prefix else "") + "origin.in": "physical source\n"},
                ), MODULE)
                origin = "$(S)/" + (prefix + "/" if prefix else "") + "origin.in"
                copied = lib.node_by_output(graph, f"$(B)/{MODULE}/parts.in")
                produced = lib.node_by_output(graph, f"$(B)/{MODULE}/parts.0.cpp")
                self.assertEqual(copied["cmds"][0]["cmd_args"][-2:], [origin, f"$(B)/{MODULE}/parts.in"])
                self.assertIn(origin, produced["inputs"])
                self.assertFalse(any(f"$(B)/{MODULE}/origin.in" in node["outputs"] for node in graph["graph"]))


if __name__ == "__main__":
    unittest.main(verbosity=2)
