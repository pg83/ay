import json
import unittest

from tst import lib


MODULE = "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"


class SourceOutputOwnershipTest(unittest.TestCase):
    def test_auto_copy_keeps_build_identity_with_a_physical_source(self):
        for copy in ("COPY_FILE(AUTO gen/dup.cpp dup.cpp)", "COPY(AUTO FROM gen dup.cpp)"):
            with self.subTest(copy=copy):
                graph = lib.make({
                    "a/ya.make": MODULE + copy + "\nEND()\n",
                    "a/dup.cpp": "int physical;\n",
                    "a/gen/dup.cpp": "int generated;\n",
                }, "a")
                compiled = lib.node_by_output(graph, "$(B)/a/dup.cpp.o")
                copied = lib.node_by_output(graph, "$(B)/a/dup.cpp")
                self.assertEqual(compiled["inputs"], ["$(B)/a/dup.cpp"])
                self.assertIn(copied["uid"], compiled["deps"])
                self.assertEqual(copied["inputs"], ["$(S)/a/gen/dup.cpp"])

    def test_duplicate_sources_skip_the_second_command(self):
        copy = "COPY_FILE(AUTO gen/dup.cpp dup.cpp)\n"
        sources = "SRCS(dup.cpp)\n"
        cases = [
            (sources + copy, True, "$(S)/a/dup.cpp"),
            (copy + sources, True, "$(S)/a/dup.cpp"),
            (sources + copy, False, "$(B)/a/dup.cpp"),
            ("SRCS(dup.cpp dup.cpp)\n", True, "$(S)/a/dup.cpp"),
        ]
        for body, physical, selected in cases:
            with self.subTest(body=body, physical=physical):
                files = {"a/ya.make": MODULE + body + "END()\n", "a/gen/dup.cpp": "int generated;\n"}
                if physical:
                    files["a/dup.cpp"] = "int physical;\n"
                strict = lib.make_process(files, "a")
                self.assertNotEqual(strict.returncode, 0)
                self.assertIn("was already added in this project", strict.stderr)
                self.assertNotIn("panic:", strict.stderr)
                kept = lib.make_process(files, "a", "-k")
                self.assertEqual(kept.returncode, 0, kept.stderr)
                self.assertIn("was already added in this project", kept.stderr)
                graph = json.loads(kept.stdout)
                compiled = lib.node_by_output(graph, "$(B)/a/dup.cpp.o")
                self.assertEqual(compiled["inputs"], [selected])
                self.assertEqual(sum("$(B)/a/dup.cpp.o" in node["outputs"] for node in graph["graph"]), 1)
                archive = lib.node_by_output(graph, "$(B)/a/liba.a")
                self.assertEqual(archive["inputs"].count("$(B)/a/dup.cpp.o"), 1)
                self.assertEqual(archive["cmds"][0]["cmd_args"].count("$(B)/a/dup.cpp.o"), 1)
                self.assertIn(compiled["uid"], archive["deps"])
                copies = [node for node in graph["graph"] if "$(B)/a/dup.cpp" in node["outputs"]]
                self.assertEqual(len(copies), int(selected.startswith("$(B)")))
                if copies:
                    self.assertIn(copies[0]["uid"], compiled["deps"])

    def test_declared_object_generator_precedes_a_source_compile(self):
        source = "SRCS(x.cpp)\n"
        producer = "RUN_PROGRAM(tools/gen OUT_NOAUTO x.cpp.o)\n"
        for body in (source + producer, producer + source):
            with self.subTest(body=body):
                files = {"a/ya.make": MODULE + body + "END()\n", "a/x.cpp": "int x;\n"}
                lib.tool_program(files, "tools/gen", "gen")
                strict = lib.make_process(files, "a")
                self.assertNotEqual(strict.returncode, 0)
                self.assertIn("was already added in this project", strict.stderr)
                self.assertNotIn("panic:", strict.stderr)
                graph = lib.make(files, "a", "-k")
                owned = [node for node in graph["graph"] if "$(B)/a/x.cpp.o" in node["outputs"]]
                self.assertEqual(len(owned), 1)
                generated = owned[0]
                self.assertEqual(generated["kv"]["p"], "PR")
                archive = lib.node_by_output(graph, "$(B)/a/liba.a")
                self.assertEqual(archive["inputs"].count("$(B)/a/x.cpp.o"), 1)
                self.assertEqual(archive["cmds"][0]["cmd_args"].count("$(B)/a/x.cpp.o"), 1)
                self.assertIn(generated["uid"], archive["deps"])

    def test_assembly_commands_use_the_same_output_ownership(self):
        source = "SRCS(x.S)\n"
        producer = "RUN_PROGRAM(tools/gen OUT_NOAUTO x.S.o)\n"
        for body, kind in (("SRCS(x.S x.S)\n", "AS"), (source + producer, "PR"), (producer + source, "PR")):
            with self.subTest(body=body):
                files = {"a/ya.make": MODULE + body + "END()\n", "a/x.S": "\n"}
                if kind == "PR":
                    lib.tool_program(files, "tools/gen", "gen")
                strict = lib.make_process(files, "a")
                self.assertNotEqual(strict.returncode, 0)
                self.assertIn("was already added in this project", strict.stderr)
                self.assertNotIn("panic:", strict.stderr)
                graph = lib.make(files, "a", "-k")
                owned = [node for node in graph["graph"] if "$(B)/a/x.S.o" in node["outputs"]]
                self.assertEqual(len(owned), 1)
                self.assertEqual(owned[0]["kv"]["p"], kind)
                if kind == "AS":
                    self.assertEqual(owned[0]["inputs"], ["$(S)/a/x.S"])
                archive = lib.node_by_output(graph, "$(B)/a/liba.a")
                self.assertEqual(archive["inputs"].count("$(B)/a/x.S.o"), 1)
                self.assertEqual(archive["cmds"][0]["cmd_args"].count("$(B)/a/x.S.o"), 1)
                self.assertIn(owned[0]["uid"], archive["deps"])


if __name__ == "__main__":
    unittest.main()
