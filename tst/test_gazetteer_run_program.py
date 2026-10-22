"""Gazetteer imports follow the module's include-resolution switch."""

import unittest

import lib


MODULE = "orchard/lexicons"
TOOL = "tools/lexicon_compiler"


def fixture(resolve=False):
    files = {
        MODULE + "/ya.make": (
            "UNION()\n" + ("SET(DONT_RESOLVE_INCLUDES no)\n" if resolve else "") +
            "SET(RESULT ${BINDIR})\n"
            f"SET(COMPILER {TOOL} -A ${{ARCADIA_ROOT}})\n"
            "RUN_PROGRAM(${COMPILER} ${CURDIR}/entries.gzt ${RESULT}/entries.gzt.bin\n"
            "  IN ${CURDIR}/entries.gzt ${ARCADIA_ROOT}/schemas/article.gztproto\n"
            "  OUT ${RESULT}/entries.gzt.bin)\nEND()\n"
        ),
        MODULE + "/entries.gzt": 'encoding "utf8";\nimport "schemas/article.gztproto";\n',
        "schemas/article.gztproto": 'import "schemas/base.proto";\nmessage Article : Base {}\n',
        "schemas/base.proto": 'import "shared/schema.proto";\nmessage Base {}\n',
        "build/sysincl/misc.yml": (
            "- includes:\n    - shared/schema.proto:\n"
            "      - vendor/first/shared/schema.proto\n"
            "      - vendor/second/shared/schema.proto\n"
            "      - vendor/third/shared/schema.proto\n"
        ),
    }
    for version in ("first", "second", "third"):
        files[f"vendor/{version}/shared/schema.proto"] = 'syntax = "proto2";\n'
    lib.tool_program(files, TOOL, "lexicon_compiler")
    return files


class GazetteerRunProgramTest(unittest.TestCase):
    def test_enabled_union_real_import_chain_and_all_common_proto_versions(self):
        graph = lib.make(fixture(resolve=True), MODULE)
        node = lib.node_by_output(graph, f"$(B)/{MODULE}/entries.gzt.bin")
        self.assertEqual(node["cmds"][0]["cmd_args"], [
            f"$(B)/{TOOL}/lexicon_compiler", "-A", "$(S)",
            f"$(S)/{MODULE}/entries.gzt", f"$(B)/{MODULE}/entries.gzt.bin",
        ])
        self.assertEqual(set(node["inputs"]), {
            f"$(B)/{TOOL}/lexicon_compiler", f"$(S)/{MODULE}/entries.gzt",
            "$(S)/schemas/article.gztproto", "$(S)/schemas/base.proto",
            *(f"$(S)/vendor/{version}/shared/schema.proto" for version in ("first", "second", "third")),
        })
        tool = lib.node_by_output(graph, f"$(B)/{TOOL}/lexicon_compiler")
        self.assertIn(tool["uid"], node["deps"])

    def test_enabled_gazetteer_reaches_schema_without_an_explicit_schema_input(self):
        files = fixture(resolve=True)
        files[MODULE + "/ya.make"] = files[MODULE + "/ya.make"].replace(
            "IN ${CURDIR}/entries.gzt ${ARCADIA_ROOT}/schemas/article.gztproto",
            "IN ${CURDIR}/entries.gzt",
        )
        graph = lib.make(files, MODULE)
        node = lib.node_by_output(graph, f"$(B)/{MODULE}/entries.gzt.bin")
        self.assertIn("$(S)/schemas/article.gztproto", node["inputs"])
        self.assertIn("$(S)/schemas/base.proto", node["inputs"])
        for version in ("first", "second", "third"):
            self.assertIn(f"$(S)/vendor/{version}/shared/schema.proto", node["inputs"])

    def test_default_union_retains_explicit_inputs_with_resolution_disabled(self):
        graph = lib.make(fixture(), MODULE)
        node = lib.node_by_output(graph, f"$(B)/{MODULE}/entries.gzt.bin")
        self.assertEqual(set(node["inputs"]), {
            f"$(B)/{TOOL}/lexicon_compiler", f"$(S)/{MODULE}/entries.gzt",
            "$(S)/schemas/article.gztproto",
        })

    def test_each_proto_family_input_parses_imports_in_an_enabled_context(self):
        for extension in ("gzt", "gztproto", "proto"):
            with self.subTest(extension=extension):
                files = fixture(resolve=True)
                path = f"schemas/article.{extension}"
                files[path] = files.pop("schemas/article.gztproto")
                files[MODULE + "/ya.make"] = files[MODULE + "/ya.make"].replace(
                    "IN ${CURDIR}/entries.gzt ${ARCADIA_ROOT}/schemas/article.gztproto",
                    "IN ${ARCADIA_ROOT}/" + path,
                )
                graph = lib.make(files, MODULE)
                node = lib.node_by_output(graph, f"$(B)/{MODULE}/entries.gzt.bin")
                self.assertIn("$(S)/schemas/base.proto", node["inputs"])
                for version in ("first", "second", "third"):
                    self.assertIn(f"$(S)/vendor/{version}/shared/schema.proto", node["inputs"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
