import unittest

import lib


class ObjectSourcesTest(unittest.TestCase):
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
