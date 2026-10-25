import unittest

from tst import lib


class ObjectSourcesTest(unittest.TestCase):
    def test_build_object_without_a_declared_producer_is_rejected(self):
        result = lib.make_process({
            "mod/ya.make": (
                f"LIBRARY()\n{lib.BARE_MODULE}"
                "SRCS(${ARCADIA_BUILD_ROOT}/mod/orphan.o)\nEND()\n"
            ),
        }, "mod")
        self.assertEqual(result.returncode, 1, result.stderr)
        self.assertIn("unsupported-source", result.stderr)
        self.assertIn("$(B)/mod/orphan.o", result.stderr)
        self.assertFalse(result.stdout)

    def test_global_physical_library_reuses_the_first_file_binding(self):
        files = {
            "shared/archive.lib": '#include "shared/detail.h"\n',
            "shared/detail.h": "// physical included header\n",
            "first/ya.make": f"LIBRARY(first)\n{lib.BARE_MODULE}SRCS(main.cpp)\nEND()\n",
            "first/main.cpp": '#include "shared/archive.lib"\nint first;\n',
            "second/ya.make": (
                f"PROGRAM(second)\n{lib.BARE_MODULE}"
                "SRCS(main.cpp GLOBAL ${ARCADIA_ROOT}/shared/archive.lib)\nEND()\n"
            ),
            "second/main.cpp": "int main() { return 0; }\n",
        }
        fixture_sources = {"$(S)/" + path for path in files}
        archive_source = "$(S)/shared/archive.lib"
        detail = "$(S)/shared/detail.h"
        link_inputs, link_dependencies = [], []
        # Native ymake establishes one shared physical file binding: whichever
        # root visits .lib first chooses C includes or the empty GLOBAL parser.
        for roots in (("first", "second"), ("second", "first")):
            with self.subTest(roots=roots):
                graph = lib.make(files, roots[1], roots[0],
                                 "--target-platform", "default-linux-x86_64")
                first_cc = lib.node_by_output(graph, "$(B)/first/main.cpp.o")
                first_ar = lib.node_by_output(graph, "$(B)/first/libfirst.a")
                second_cc = lib.node_by_output(graph, "$(B)/second/main.cpp.o")
                linked = lib.node_by_output(graph, "$(B)/second/second")
                included = [detail] if roots[0] == "first" else []
                self.assertCountEqual(first_cc["inputs"],
                                      ["$(S)/first/main.cpp", archive_source, *included])
                self.assertEqual(first_cc["deps"], [])
                self.assertEqual(first_ar["inputs"],
                                 [first_cc["outputs"][0], "$(S)/build/scripts/link_lib.py"])
                self.assertEqual(first_ar["deps"], [first_cc["uid"]])
                self.assertEqual(second_cc["inputs"], ["$(S)/second/main.cpp"])
                self.assertEqual(second_cc["deps"], [])
                for node, expected in (
                    (first_cc, ["$(S)/first/main.cpp", archive_source, *included]),
                    (first_ar, []),
                    (second_cc, ["$(S)/second/main.cpp"]),
                    (linked, [archive_source, *included]),
                ):
                    self.assertCountEqual(
                        [path for path in node["inputs"] if path in fixture_sources], expected)
                nodes = {node["uid"]: node for node in graph["graph"]}
                link_dependencies.append([nodes[ref]["outputs"] for ref in linked["deps"]])
                link_inputs.append(linked["inputs"])
        self.assertEqual(link_dependencies[0], link_dependencies[1])
        self.assertCountEqual(link_inputs[0], [*link_inputs[1], detail])

    def test_generated_object_declarations_keep_each_archive_scope_and_order(self):
        graph = lib.make({
            "mod/ya.make": (
                f"LIBRARY()\n{lib.BARE_MODULE}"
                "COPY_FILE(input.bin prepared.o)\n"
                "SRCS(GLOBAL ${BINDIR}/prepared.o)\n"
                "SRCS(native.cpp)\n"
                "SRCS(GLOBAL ${BINDIR}/prepared.o)\nEND()\n"
            ),
            "mod/input.bin": "object\n",
            "mod/native.cpp": "int native;\n",
        }, "mod", "--target-platform", "default-linux-x86_64")
        copied = lib.node_by_output(graph, "$(B)/mod/prepared.o")
        compiled = lib.node_by_output(graph, "$(B)/mod/native.cpp.o")
        local = lib.node_by_output(graph, "$(B)/mod/libmod.a")
        glob = lib.node_by_output(graph, "$(B)/mod/libmod.global.a")
        self.assertEqual(local["cmds"][0]["cmd_args"][10:],
                         [copied["outputs"][0], compiled["outputs"][0]])
        self.assertEqual(glob["cmds"][0]["cmd_args"][10:], [copied["outputs"][0]] * 2)
        for archive in (local, glob):
            self.assertEqual(archive["inputs"].count(copied["outputs"][0]), 1)
            self.assertEqual(archive["deps"].count(copied["uid"]), 1)
        self.assertNotIn(compiled["uid"], glob["deps"])

    def test_physical_object_is_an_archive_input_without_a_producer(self):
        graph = lib.make({
            "mod/ya.make": (
                f"LIBRARY()\n{lib.BARE_MODULE}"
                "SRCS(GLOBAL prepared.o)\nSRCS(native.cpp)\nEND()\n"
            ),
            "mod/prepared.o": "object\n",
            "mod/native.cpp": "int native;\n",
        }, "mod", "--target-platform", "default-linux-x86_64")
        local = lib.node_by_output(graph, "$(B)/mod/libmod.a")
        glob = lib.node_by_output(graph, "$(B)/mod/libmod.global.a")
        compiled = lib.node_by_output(graph, "$(B)/mod/native.cpp.o")
        self.assertEqual(local["cmds"][0]["cmd_args"][10:], [compiled["outputs"][0]])
        self.assertEqual(glob["cmds"][0]["cmd_args"][10:], ["$(S)/mod/prepared.o"])
        self.assertIn("$(S)/mod/prepared.o", glob["inputs"])
        self.assertEqual(glob["deps"], [])
        ids = {node["uid"] for node in graph["graph"]}
        self.assertTrue(all(set(node["deps"]) <= ids for node in graph["graph"]))

    def test_physical_obj_is_a_global_archive_input(self):
        graph = lib.make({
            "mod/ya.make": f"LIBRARY()\n{lib.BARE_MODULE}SRCS(GLOBAL prepared.obj)\nEND()\n",
            "mod/prepared.obj": "object\n",
        }, "mod", "--target-platform", "default-linux-x86_64")
        glob = lib.node_by_output(graph, "$(B)/mod/libmod.global.a")
        self.assertEqual(glob["cmds"][0]["cmd_args"][10:], ["$(S)/mod/prepared.obj"])
        self.assertIn("$(S)/mod/prepared.obj", glob["inputs"])
        self.assertEqual(glob["deps"], [])

    def test_physical_and_generated_global_objects_keep_only_real_producer_dependencies(self):
        for objects in (
            ("physical.o", "${BINDIR}/first.o", "${BINDIR}/second.o"),
            ("${BINDIR}/first.o", "physical.o", "${BINDIR}/second.o"),
        ):
            with self.subTest(objects=objects):
                files = {
                    "mod/ya.make": (
                        f"LIBRARY()\n{lib.BARE_MODULE}"
                        "RUN_PROGRAM(tools/scribe IN input.bin OUT_NOAUTO first.o)\n"
                        "RUN_PROGRAM(tools/scribe IN input.bin OUT_NOAUTO second.o)\n"
                        + "SRCS(" + " ".join("GLOBAL " + name for name in objects) + ")\nEND()\n"
                    ),
                    "mod/physical.o": "object\n",
                    "mod/input.bin": "object\n",
                }
                lib.tool_program(files, "tools/scribe", "scribe")
                files["tools/scribe/ya.make"] = files["tools/scribe/ya.make"].replace(
                    "SRCS(main.cpp)", "ALLOCATOR(FAKE)\nSRCS(main.cpp)",
                )
                graph = lib.make(files, "mod", "--sandboxing",
                                 "--target-platform", "default-linux-x86_64",
                                 "--host-platform", "default-linux-x86_64")
                archive = lib.node_by_output(graph, "$(B)/mod/libmod.global.a")
                expected = ["$(S)/mod/physical.o" if name == "physical.o" else
                            "$(B)/mod/" + name.rsplit("/", 1)[-1] for name in objects]
                self.assertEqual(archive["cmds"][0]["cmd_args"][10:], expected)
                self.assertCountEqual(archive["inputs"], [*expected, "$(S)/build/scripts/link_lib.py"])
                producers = [lib.node_by_output(graph, "$(B)/mod/" + name)
                             for name in ("first.o", "second.o")]
                self.assertCountEqual(archive["deps"], [node["uid"] for node in producers])

    def test_physical_objects_are_program_and_dll_inputs(self):
        for module, output in (("PROGRAM(app)", "app"), ("DLL(plugin)", "libplugin.so")):
            with self.subTest(module=module):
                files = {
                    "mod/ya.make": (
                        f"{module}\n{lib.BARE_MODULE}"
                        "SRCS(local.o GLOBAL prepared.o)\nEND()\n"
                    ),
                    "mod/local.o": "object\n",
                    "mod/prepared.o": "object\n",
                }
                lib.tool_program(files, "tools/fix_elf", "fix_elf")
                graph = lib.make(files, "mod", "--target-platform", "default-linux-x86_64")
                linked = lib.node_by_output(graph, "$(B)/mod/" + output)
                for name in ("local.o", "prepared.o"):
                    source = "$(S)/mod/" + name
                    argument = source if name == "local.o" else "mod/" + name
                    self.assertEqual(linked["inputs"].count(source), 1)
                    self.assertTrue(any(argument in command["cmd_args"] for command in linked["cmds"]))
                ids = {node["uid"] for node in graph["graph"]}
                self.assertTrue(all(set(node["deps"]) <= ids for node in graph["graph"]))


if __name__ == "__main__":
    unittest.main(verbosity=2)
