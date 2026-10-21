import unittest

import lib
from test_resource_files import base_files, objcopy_nodes, payload


PYTHON = "$(B)/resources/YMAKE_PYTHON3/bin/python3"
SCRIPT = "$(S)/contrib/tools/python3/Tools/i18n/msgfmt.py"
SOURCE = "$(S)/lib/sub/messages.po"


class GettextTest(unittest.TestCase):
    def test_catalogue_output_paths_and_resource_dependencies(self):
        for keep, directory in (("no", ""), ("true", ""), ("yes", "sub/")):
            with self.subTest(keep=keep):
                output = "$(B)/lib/" + directory + "messages.mo"
                files = base_files(
                    f"SET(GETTEXT_KEEP_PATH {keep})\nSRCS(sub/messages.po)\n"
                    f"RESOURCE({directory}messages.mo /messages)\n"
                )
                files["lib/sub/messages.po"] = 'msgid "hello"\nmsgstr "world"\n'
                files[SCRIPT.removeprefix("$(S)/")] = ""
                graph = lib.make(files, "lib")
                catalogue = lib.node_by_output(graph, output)
                self.assertEqual(catalogue["kv"], {"p": "PO", "pc": "yellow"})
                self.assertEqual(catalogue["cmds"][0]["cmd_args"], [PYTHON, SCRIPT, "-o", output, SOURCE])
                self.assertEqual(catalogue["inputs"], [SCRIPT, SOURCE])
                resource = next(n for n in objcopy_nodes(graph) if output in n["inputs"])
                self.assertIn(catalogue["uid"], resource["deps"])
                self.assertEqual(payload(resource)[0], [output])
                self.assertFalse(any(output + ".o" in n["outputs"] for n in graph["graph"]))

    def test_keep_path_uses_the_resolved_source_directory(self):
        files = base_files("SET(GETTEXT_KEEP_PATH yes)\nSRCDIR(external)\nSRCS(sub/messages.po)\nRESOURCE(${ARCADIA_BUILD_ROOT}/external/sub/messages.mo /messages)\n")
        files["external/sub/messages.po"] = ""
        files[SCRIPT.removeprefix("$(S)/")] = ""
        graph = lib.make(files, "lib")
        catalogue = lib.node_by_output(graph, "$(B)/external/sub/messages.mo")
        self.assertEqual(catalogue["cmds"][0]["cmd_args"][-1], "$(S)/external/sub/messages.po")
        resource = next(n for n in graph["graph"] if n["kv"]["p"] == "PR" and "$(B)/external/sub/messages.mo" in n["inputs"])
        self.assertIn(catalogue["uid"], resource["deps"])

    def test_unused_catalogue_output_is_not_emitted_or_archived(self):
        files = base_files("SRCS(sub/messages.po native.cpp)\n")
        files["lib/sub/messages.po"] = ""
        files[SCRIPT.removeprefix("$(S)/")] = ""
        files["lib/native.cpp"] = "int value;\n"
        graph = lib.make(files, "lib")
        lib.node_by_output(graph, "$(B)/lib/native.cpp.o")
        self.assertFalse(any(n["kv"]["p"] == "PO" for n in graph["graph"]))
        archive = lib.node_by_output_prefix(graph, "$(B)/lib/lib")
        self.assertFalse(any(a.endswith(".mo") for c in archive["cmds"] for a in c["cmd_args"]))

    def test_source_precedes_a_same_path_producer_unless_build_root_is_explicit(self):
        for token, input in (("same.po", "$(S)/lib/same.po"), ("${BINDIR}/same.po", "$(B)/lib/same.po")):
            with self.subTest(token=token):
                files = base_files(f"SRCS({token})\nRUN_PROGRAM(tools/gen OUT_NOAUTO same.po)\nRESOURCE(same.mo /messages)\n")
                files["lib/same.po"] = ""
                files[SCRIPT.removeprefix("$(S)/")] = ""
                lib.tool_program(files, "tools/gen", "gen")
                graph = lib.make(files, "lib")
                catalogue = lib.node_by_output(graph, "$(B)/lib/same.mo")
                self.assertEqual(catalogue["cmds"][0]["cmd_args"][-1], input)
                self.assertIn(input, catalogue["inputs"])
                generated = [n for n in graph["graph"] if "$(B)/lib/same.po" in n["outputs"]]
                if token == "same.po":
                    self.assertEqual(generated, [])
                else:
                    self.assertEqual(len(generated), 1)
                    self.assertIn(generated[0]["uid"], catalogue["deps"])

    def test_noauto_generated_po_is_consumed_without_catalogue_processing(self):
        files = base_files("RUN_PROGRAM(tools/gen OUT_NOAUTO generated.po)\nRESOURCE(generated.po /po)\n")
        files[SCRIPT.removeprefix("$(S)/")] = ""
        lib.tool_program(files, "tools/gen", "gen")
        graph = lib.make(files, "lib")
        generated = lib.node_by_output(graph, "$(B)/lib/generated.po")
        resource = next(n for n in objcopy_nodes(graph) if "$(B)/lib/generated.po" in n["inputs"])
        self.assertIn(generated["uid"], resource["deps"])
        self.assertFalse(any(n["kv"]["p"] == "PO" for n in graph["graph"]))

    def test_missing_po_source_requires_a_declared_producer(self):
        files = base_files("SRCS(missing.po)\nRESOURCE(missing.mo /messages)\n")
        files[SCRIPT.removeprefix("$(S)/")] = ""
        result = lib.make_process(files, "lib")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("missing.po", result.stderr)
        self.assertNotIn("panic:", result.stderr)

    def test_explicit_po_source_waits_for_its_declared_generator(self):
        files = base_files("SRCS(generated.po)\nRUN_PROGRAM(tools/gen OUT_NOAUTO generated.po)\nRESOURCE(generated.mo /messages)\n")
        files[SCRIPT.removeprefix("$(S)/")] = ""
        lib.tool_program(files, "tools/gen", "gen")
        graph = lib.make(files, "lib")
        generated = lib.node_by_output(graph, "$(B)/lib/generated.po")
        catalogue = lib.node_by_output(graph, "$(B)/lib/generated.mo")
        self.assertEqual(catalogue["cmds"][0]["cmd_args"][-1], "$(B)/lib/generated.po")
        self.assertIn(generated["uid"], catalogue["deps"])

    def test_codegen_consumer_declared_first_waits_for_the_catalogue(self):
        files = base_files("RUN_PROGRAM(tools/gen IN messages.mo OUT data.txt)\nSRCS(messages.po)\nRESOURCE(data.txt /data)\n")
        files["lib/messages.po"] = ""
        files[SCRIPT.removeprefix("$(S)/")] = ""
        lib.tool_program(files, "tools/gen", "gen")
        graph = lib.make(files, "lib")
        catalogue = lib.node_by_output(graph, "$(B)/lib/messages.mo")
        consumer = lib.node_by_output(graph, "$(B)/lib/data.txt")
        self.assertIn("$(B)/lib/messages.mo", consumer["inputs"])
        self.assertIn(catalogue["uid"], consumer["deps"])

    def test_generated_po_source_keeps_its_existing_producer(self):
        files = base_files("RUN_PROGRAM(tools/gen OUT generated.po)\nRESOURCE(generated.mo /messages)\n")
        files[SCRIPT.removeprefix("$(S)/")] = ""
        lib.tool_program(files, "tools/gen", "gen")
        graph = lib.make(files, "lib")
        generated = lib.node_by_output(graph, "$(B)/lib/generated.po")
        catalogue = lib.node_by_output(graph, "$(B)/lib/generated.mo")
        self.assertIn(generated["uid"], catalogue["deps"])
        self.assertEqual(catalogue["cmds"][0]["cmd_args"][-1], "$(B)/lib/generated.po")
        resource = next(n for n in objcopy_nodes(graph) if "$(B)/lib/generated.mo" in n["inputs"])
        self.assertIn(catalogue["uid"], resource["deps"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
