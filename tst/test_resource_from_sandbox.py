import unittest

import lib


BARE = "NO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
SCRIPTS = [
    "$(S)/build/scripts/fetch_from_sandbox.py",
    "$(S)/build/scripts/process_command_files.py",
    "$(S)/build/scripts/fetch_from.py",
]


def sandbox_nodes(graph):
    return [node for node in graph["graph"] if node["kv"]["p"] == "SB"]


class FromSandboxTest(unittest.TestCase):
    def test_untar_with_renames_outputs_and_includes(self):
        graph = lib.make({
            "lib/ya.make": (
                f"LIBRARY()\n{BARE}"
                "SRCS(user.cpp)\n"
                "FROM_SANDBOX(\n"
                "  123 AUTOUPDATED updater PREFIX pre\n"
                "  RENAME a.bin b.bin\n"
                "  OUT data.h libx.a\n"
                "  OUT_NOAUTO extra.h\n"
                "  OUTPUT_INCLUDES lib/dep.h ${ARCADIA_ROOT}/lib/dep2.h\n"
                "  INDUCED_DEPS $SOME_DEPS\n"
                "  EXECUTABLE\n"
                ")\n"
                "END()\n"
            ),
            "lib/user.cpp": '#include "extra.h"\nint u(){return 0;}\n',
            "lib/dep.h": "",
            "lib/dep2.h": "",
        }, "lib")
        [sandbox] = sandbox_nodes(graph)
        self.assertEqual(sandbox["outputs"], [
            "$(B)/lib/data.h",
            "$(B)/lib/libx.a",
            "$(B)/lib/extra.h",
        ])
        cmd = sandbox["cmds"][0]
        self.assertEqual(cmd["cmd_args"][1:], [
            "$(S)/build/scripts/fetch_from_sandbox.py",
            "--ya-start-command-file",
            "--resource-file", "$(RESOURCE_ROOT)/sbr/123/resource",
            "--resource-id", "123",
            "--untar-to", "pre",
            "--rename", "a.bin",
            "--rename", "b.bin",
            "--executable",
            "--",
            "data.h", "libx.a", "extra.h",
            "--ya-end-command-file",
        ])
        self.assertEqual(cmd["cwd"], "$(B)/lib")
        self.assertEqual(sandbox["inputs"], SCRIPTS)
        self.assertEqual(sandbox["kv"], {"p": "SB", "pc": "yellow", "show_out": "yes"})
        self.assertEqual(
            sandbox["requirements"],
            {"cpu": 1, "network": "full", "ram": 32},
        )

        compile_node = lib.node_by_output(graph, "$(B)/lib/user.cpp.o")
        self.assertEqual(compile_node["inputs"], [
            "$(S)/lib/user.cpp",
            "$(B)/lib/extra.h",
            "$(S)/lib/dep.h",
            "$(S)/lib/dep2.h",
        ])
        self.assertIn(sandbox["uid"], compile_node["deps"])

        archive = lib.node_by_output(graph, "$(B)/lib/liblib.a")
        self.assertIn("$(B)/lib/libx.a", archive["inputs"])
        self.assertIn(sandbox["uid"], archive["deps"])

    def test_file_mode_is_emitted_only_when_consumed(self):
        files = {
            "library/cpp/resource/ya.make": f"LIBRARY()\n{BARE}END()\n",
            "lib/ya.make": (
                f"LIBRARY()\n{BARE}"
                "FROM_SANDBOX(FILE 456 SBR sbr: OUT_NOAUTO blob.dat)\n"
                "FROM_SANDBOX(789 OUT_NOAUTO unused.dat)\n"
                "RESOURCE(${BINDIR}/blob.dat key)\n"
                "END()\n"
            ),
        }
        lib.tool_program(files, "tools/rescompiler", "rescompiler")
        lib.tool_program(files, "tools/rescompressor", "rescompressor")
        graph = lib.make(files, "lib")
        [sandbox] = sandbox_nodes(graph)
        self.assertEqual(sandbox["outputs"], ["$(B)/lib/blob.dat"])
        self.assertEqual(sandbox["cmds"][0]["cmd_args"][5:], [
            "--resource-id", "456",
            "--copy-to-dir", ".",
            "--",
            "blob.dat",
            "--ya-end-command-file",
        ])
        objcopy = lib.node_by_output_prefix(graph, "$(B)/lib/objcopy_")
        self.assertEqual(objcopy["inputs"], [
            "$(B)/tools/rescompiler/rescompiler",
            "$(B)/tools/rescompressor/rescompressor",
            "$(B)/lib/blob.dat",
            "$(S)/build/scripts/objcopy.py",
        ])
        self.assertIn(sandbox["uid"], objcopy["deps"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
