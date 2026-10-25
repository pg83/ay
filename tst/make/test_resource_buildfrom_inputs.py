"""Pinned d02375f881ac: json_visitor.cpp PrepareLeaving, lines 697-713.

Native included inputs propagate over BuildFrom, not OutTogether.

The producer's primary output remains a resource input/dependency. Only genuine
resource payloads can trigger the native .in IncludedInputs forwarding rule.
"""
import unittest

from tst import lib


PREFIX = "$(B)/grove/ledger/"


def fixture(outputs, payload, extra=""):
    files = {
        "grove/ledger/ya.make": (
            f"LIBRARY(ledger)\n{lib.BARE_MODULE}"
            f"RUN_PROGRAM(tools/writer OUT_NOAUTO {outputs})\n{extra}"
            f"RESOURCE(${{BINDIR}}/{payload} /payload)\n"
            "SRCS(local.cpp GLOBAL stone.cpp)\nEND()\n"
        ),
        "grove/ledger/local.cpp": "",
        "grove/ledger/stone.cpp": "",
        "build/scripts/objcopy.py": "",
        "library/cpp/resource/ya.make": f"LIBRARY()\n{lib.BARE_MODULE}END()\n",
    }
    for name in ("writer", "rescompiler", "rescompressor"):
        lib.tool_program(files, "tools/" + name, name)
    return files


class ResourceBuildFromInputsTest(unittest.TestCase):
    def check(self, files, primary, payload, forwarded):
        graph = lib.make(files, "grove/ledger")
        producer = lib.node_by_output(graph, PREFIX + primary)
        resource = lib.node_by_output_prefix(graph, PREFIX + "objcopy_")
        archive = lib.node_by_output(graph, PREFIX + "libledger.global.a")
        ordinary = lib.node_by_output(graph, PREFIX + "libledger.a")
        self.assertIn(PREFIX + primary, resource["inputs"])
        self.assertIn(PREFIX + payload, resource["inputs"])
        self.assertIn(producer["uid"], resource["deps"])
        self.assertIn(resource["uid"], archive["deps"])
        for name in ("rescompiler", "rescompressor"):
            tool = lib.node_by_output(graph, f"$(B)/tools/{name}/{name}")
            self.assertIn(tool["uid"], resource["deps"])
        if forwarded:
            self.assertIn(producer["uid"], archive["deps"])
        self.assertEqual(
            set(archive["inputs"]),
            {resource["outputs"][0], PREFIX + "stone.cpp.o", "$(S)/build/scripts/link_lib.py", *forwarded},
        )
        self.assertEqual(set(ordinary["inputs"]), {PREFIX + "local.cpp.o", "$(S)/build/scripts/link_lib.py"})
        return graph, resource, archive

    def test_auxiliary_primary_template_is_not_a_resource_buildfrom_edge(self):
        for primary in ("anchor.in", "anchor.dat"):
            with self.subTest(primary=primary):
                self.check(fixture(primary + " payload.json", "payload.json"), primary, "payload.json", [])

    def test_real_primary_and_secondary_templates_keep_owned_producer_inputs(self):
        for primary, payload, forwarded in (
            ("anchor.in", "anchor.in", [PREFIX + "anchor.in"]),
            ("anchor.dat", "payload.in", [PREFIX + "payload.in", PREFIX + "anchor.dat"]),
        ):
            with self.subTest(primary=primary, payload=payload):
                self.check(fixture(primary + " payload.in", payload), primary, payload, forwarded)

    def test_copied_source_template_keeps_source_handoff_and_producer(self):
        files = fixture("anchor.in payload.json", "copied.in", "COPY_FILE(${CURDIR}/seed.in copied.in)\n")
        files["grove/ledger/seed.in"] = "source template\n"
        # The unused generator is intentionally independent of the copied payload.
        graph = lib.make(files, "grove/ledger")
        copy = lib.node_by_output(graph, PREFIX + "copied.in")
        resource = lib.node_by_output_prefix(graph, PREFIX + "objcopy_")
        archive = lib.node_by_output(graph, PREFIX + "libledger.global.a")
        self.assertIn(copy["uid"], resource["deps"])
        self.assertIn(PREFIX + "copied.in", resource["inputs"])
        self.assertIn("$(S)/grove/ledger/seed.in", resource["inputs"])
        self.assertIn(PREFIX + "copied.in", archive["inputs"])
        self.assertIn("$(S)/grove/ledger/seed.in", archive["inputs"])
        self.assertNotIn(PREFIX + "anchor.in", archive["inputs"])

    def test_physical_source_template_remains_an_archive_input(self):
        files = fixture("anchor.in payload.json", "seed.in")
        files["grove/ledger/ya.make"] = files["grove/ledger/ya.make"].replace(
            "RESOURCE(${BINDIR}/seed.in", "RESOURCE(${CURDIR}/seed.in",
        )
        files["grove/ledger/seed.in"] = "source template\n"
        graph = lib.make(files, "grove/ledger")
        resource = lib.node_by_output_prefix(graph, PREFIX + "objcopy_")
        archive = lib.node_by_output(graph, PREFIX + "libledger.global.a")
        self.assertIn("$(S)/grove/ledger/seed.in", resource["inputs"])
        self.assertEqual(set(archive["inputs"]), {
            resource["outputs"][0], PREFIX + "stone.cpp.o",
            "$(S)/grove/ledger/seed.in", "$(S)/build/scripts/link_lib.py",
        })


if __name__ == '__main__':
    unittest.main()
