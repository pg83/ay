"""Native ARCHIVE configuration and Required command-tool dependencies."""
import json
import unittest

import lib
from test_resource_run_program import library


FLAGS = ("-DIS_CROSS_TOOLS=no", "--host-platform-flag", "IS_CROSS_TOOLS=yes")
BODY = (
    "RUN_PROGRAM(tools/gen IN input.cpp OUT_NOAUTO main.cpp data.bin)\n"
    "ARCHIVE(NAME data.inc DONTCOMPRESS data.bin data.bin)\n"
)


def fixture(consumed=False):
    files = library(BODY, input__cpp='#include "private.h"\n', private__h="")
    lib.tool_program(files, "tools/archiver", "archiver")
    if consumed:
        files["lib/ya.make"] = files["lib/ya.make"].replace("END()", "SRCS(use.cpp)\nEND()")
        files["lib/use.cpp"] = '#include "data.inc"\n'
    return files


class ArchiveConfigurationTest(unittest.TestCase):
    def assert_empty(self, process):
        self.assertEqual(process.returncode, 0, process.stderr)
        self.assertNotIn("tool-conf:", process.stderr)
        graph = json.loads(process.stdout)
        self.assertEqual(graph["result"], [])
        self.assertEqual(graph["graph"], [])

    def assert_failed(self, process, diagnostic):
        self.assertNotEqual(process.returncode, 0)
        self.assertIn(diagnostic, process.stderr)
        self.assertNotIn("panic:", process.stderr)

    def test_unused_archive_and_program_outputs_stay_unpublished(self):
        self.assert_empty(lib.make_process(fixture(), "lib", *FLAGS))

    def test_tools_configure_target_and_require_host_only_for_consumers(self):
        for tool in ("gen", "archiver"):
            for consumed in (False, True):
                for context, condition in (("target", "NOT IS_CROSS_TOOLS"), ("host", "IS_CROSS_TOOLS")):
                    with self.subTest(tool=tool, consumed=consumed, context=context):
                        files = fixture(consumed)
                        path = f"tools/{tool}/ya.make"
                        files[path] = files[path].replace(
                            "END()", f"IF({condition})\nPEERDIR(missing/{context}_peer)\nENDIF()\nEND()"
                        )
                        process = lib.make_process(files, "lib", *FLAGS)
                        if consumed or context == "target":
                            self.assert_failed(process, f"missing/{context}_peer")
                        else:
                            self.assert_empty(process)

    def test_unused_archive_validates_member_and_tools(self):
        for missing in ("member", "archiver", "gen", "aux"):
            with self.subTest(missing=missing):
                files = fixture()
                if missing == "member":
                    files["lib/ya.make"] = files["lib/ya.make"].replace(
                        "DONTCOMPRESS data.bin data.bin", "DONTCOMPRESS missing.bin"
                    )
                    diagnostic = "missing.bin"
                elif missing == "aux":
                    files["lib/ya.make"] = files["lib/ya.make"].replace(
                        "RUN_PROGRAM(tools/gen IN", "RUN_PROGRAM(tools/gen TOOL tools/absent IN"
                    )
                    diagnostic = "tools/absent"
                else:
                    files = {name: text for name, text in files.items() if not name.startswith(f"tools/{missing}/")}
                    diagnostic = f"tools/{missing}"
                self.assert_failed(lib.make_process(files, "lib", *FLAGS), diagnostic)

    def test_unused_program_validates_main_and_auxiliary_tool_configuration(self):
        for invalid in ("missing", "peer", "copy", "aux"):
            with self.subTest(invalid=invalid):
                files = fixture()
                files["lib/ya.make"] = files["lib/ya.make"].replace(BODY, BODY.split("ARCHIVE(")[0])
                if invalid == "missing":
                    files = {name: text for name, text in files.items() if not name.startswith("tools/gen/")}
                    diagnostic = "tools/gen"
                elif invalid == "aux":
                    files["lib/ya.make"] = files["lib/ya.make"].replace(
                        "RUN_PROGRAM(tools/gen IN", "RUN_PROGRAM(tools/gen TOOL tools/absent IN"
                    )
                    diagnostic = "tools/absent"
                else:
                    statement = "PEERDIR(missing/tool_peer)" if invalid == "peer" else "COPY_FILE(only-one)"
                    files["tools/gen/ya.make"] = files["tools/gen/ya.make"].replace("END()", f"{statement}\nEND()")
                    diagnostic = "missing/tool_peer" if invalid == "peer" else "COPY_FILE"
                self.assert_failed(lib.make_process(files, "lib", *FLAGS), diagnostic)

    def test_nested_archives_activate_members_through_ordinary_node_inputs(self):
        files = fixture()
        files["lib/ya.make"] = files["lib/ya.make"].replace(
            "END()", "ARCHIVE(NAME outer.inc DONTCOMPRESS data.inc data.inc)\nSRCS(use.cpp)\nEND()"
        )
        files["lib/use.cpp"] = '#include "outer.inc"\n'
        graph = lib.make(files, "lib", *FLAGS)
        producer = lib.node_by_output(graph, "$(B)/lib/data.bin")
        inner = lib.node_by_output(graph, "$(B)/lib/data.inc")
        outer = lib.node_by_output(graph, "$(B)/lib/outer.inc")
        consumer = lib.node_by_output(graph, "$(B)/lib/use.cpp.o")
        self.assertIn(producer["uid"], inner["deps"])
        self.assertIn(inner["uid"], outer["deps"])
        self.assertIn(outer["uid"], consumer["deps"])
        self.assertIn("$(B)/lib/main.cpp", inner["inputs"])
        self.assertNotIn("$(B)/lib/main.cpp", inner["cmds"][0]["cmd_args"])
        self.assertEqual(outer["cmds"][0]["cmd_args"].count("$(B)/lib/data.inc:"), 2)
        tool = lib.node_by_output(graph, "$(B)/tools/archiver/archiver")
        for archive in (inner, outer):
            self.assertEqual(len(archive["foreign_deps"]["tool"]), 1)
            self.assertIn(tool["uid"], archive["deps"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
