import unittest

from tst import lib


BARE = "NO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"


def with_tools(files):
    lib.tool_program(files, "contrib/tools/yasm", "yasm")
    lib.tool_program(files, "tools/generate", "generate")
    return files


class AssemblyPathsTest(unittest.TestCase):
    def test_yasm_srcdir_outputs_follow_resolved_source_paths(self):
        for source_dir in ("shared", "${ARCADIA_ROOT}/shared"):
            with self.subTest(source_dir=source_dir):
                graph = lib.make(with_tools({
                    "mod/ya.make": "LIBRARY()\n" + BARE + f"SRCDIR({source_dir})\nSRCS(x86/routine.asm)\nEND()\n",
                    "shared/x86/routine.asm": "",
                }), "mod")
                node = lib.node_by_output(graph, "$(B)/mod/__/shared/x86/routine.o")
                self.assertEqual(node["cmds"][0]["cmd_args"][-3:], [
                    "-o", "$(B)/mod/__/shared/x86/routine.o", "$(S)/shared/x86/routine.asm",
                ])
                archive = lib.node_by_output(graph, "$(B)/mod/libmod.a")
                self.assertIn(node["outputs"][0], archive["inputs"])
                self.assertIn(node["uid"], archive["deps"])

    def test_yasm_parent_relative_sources_keep_parent_path_encoding(self):
        for source in ("../shared/routine.yasm", "${ARCADIA_ROOT}/shared/routine.yasm"):
            with self.subTest(source=source):
                graph = lib.make(with_tools({
                    "mod/ya.make": "LIBRARY()\n" + BARE + f"SRCS({source} ../mod/local.asm)\nEND()\n",
                    "shared/routine.yasm": "",
                    "mod/local.asm": "",
                }), "mod")
                node = lib.node_by_output(graph, "$(B)/mod/__/shared/routine.o")
                self.assertEqual(node["cmds"][0]["cmd_args"][-1], "$(S)/shared/routine.yasm")
                local = lib.node_by_output(graph, "$(B)/mod/local.o")
                self.assertEqual(local["cmds"][0]["cmd_args"][-1], "$(S)/mod/local.asm")

    def test_generated_assembly_outside_module_keeps_producer_and_relative_path(self):
        for extension in ("asm", "S"):
            with self.subTest(extension=extension):
                generated = "${ARCADIA_BUILD_ROOT}/shared/generated." + extension
                graph = lib.make(with_tools({
                    "mod/ya.make": "LIBRARY()\n" + BARE + f"RUN_PROGRAM(tools/generate OUT_NOAUTO {generated})\nSRCS({generated})\nEND()\n",
                }), "mod")
                output = "$(B)/mod/__/shared/generated" + (".o" if extension == "asm" else ".S.o")
                node = lib.node_by_output(graph, output)
                producer = lib.node_by_output(graph, "$(B)/shared/generated." + extension)
                self.assertIn(producer["outputs"][0], node["inputs"])
                self.assertIn(producer["uid"], node["deps"])
                archive = lib.node_by_output(graph, "$(B)/mod/libmod.a")
                self.assertIn(output, archive["inputs"])

    def test_yasm_noext_removes_only_the_last_extension(self):
        graph = lib.make(with_tools({
            "mod/ya.make": "LIBRARY()\n" + BARE + "SRCS(routine.asm.yasm)\nEND()\n",
            "mod/routine.asm.yasm": "",
        }), "mod")
        node = lib.node_by_output(graph, "$(B)/mod/routine.asm.o")
        self.assertEqual(node["cmds"][0]["cmd_args"][-1], "$(S)/mod/routine.asm.yasm")

    def test_gnu_srcdir_control_keeps_source_extension(self):
        graph = lib.make(with_tools({
            "mod/ya.make": "LIBRARY()\n" + BARE + "SRCDIR(shared)\nSRCS(x86/routine.S)\nEND()\n",
            "shared/x86/routine.S": "",
        }), "mod")
        node = lib.node_by_output(graph, "$(B)/mod/__/shared/x86/routine.S.o")
        self.assertIn("$(S)/shared/x86/routine.S", node["inputs"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
