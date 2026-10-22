"""RESOLVE_PROTO evidence from the pinned source tree at /home/pg/2.

build/conf/proto.conf:668-671 defines exactly:
    macro RESOLVE_PROTO() {
        SET(DONT_RESOLVE_INCLUDES no)
        ADDINCL(FOR proto $PROTOBUF_INCLUDE_PATH)
    }
There is no producer, command, or extra peer in the macro. Lines 662-667
explicitly enable resolution for every supported language, not only proto.
Line 21 supplies the stock contrib/libs/protobuf/src include directory.

build/ymake.core.conf:2614-2633 makes UNION default to
SET(DONT_RESOLVE_INCLUDES yes) and makes ordinary file copies TEXT.
devtools/ymake/module_state.cpp:339 computes DontResolveIncludes as
Vars.Contains(VAR_DONT_RESOLVE_INCLUDES) && Get(VAR_DONT_RESOLVE_INCLUDES) != "no".
Thus an absent variable enables resolution; empty and yes disable it; no
enables it. devtools/ymake/module_resolver.cpp:247-274 checks that flag before
resolving and chooses the language from the source extension/parser.
devtools/ymake/include_processors/proto_processor.cpp:43-48 resolves proto
imports using LanguageId, independently of the command consuming the file.

Source hashes for the macro/default and UNION definition, respectively:
466024e53fca654c54cf923cea103128a290ead341e9c3eb7987a351b1f75227
efc8327476088319ee89eac362f01a0845c89863621b4a8e320a8ef38639989f
"""

import json
import unittest

import lib


STOCK = "contrib/libs/protobuf/src"


def fixture(body="RESOLVE_PROTO()\n", module="UNION()"):
    files = {
        "parcel/ya.make": (
            f"{module}\n{lib.BARE_MODULE}"
            "ADDINCL(headers)\n"
            + body
            + "RUN_PROGRAM(tools/packer --emit parcel.bin "
            "IN catalog.gzt bridge.h OUT parcel.bin)\nEND()\n"
        ),
        "parcel/catalog.gzt": 'import "google/protobuf/parcel.proto";\n',
        f"{STOCK}/google/protobuf/parcel.proto": 'import "google/protobuf/leaf.proto";\n',
        f"{STOCK}/google/protobuf/leaf.proto": "message Leaf {}\n",
        "parcel/bridge.h": '#include <detail.h>\n',
        "headers/detail.h": "#pragma once\n",
    }
    lib.tool_program(files, "tools/packer", "packer")
    return files


class ResolveProtoTest(unittest.TestCase):
    def graph(self, files, *targets):
        result = lib.make_process(files, targets[0], *targets[1:])
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertNotIn("unknown-macro", result.stderr)
        self.assertNotIn("missing-include", result.stderr)
        return json.loads(result.stdout)

    def test_union_enables_all_languages_and_transitive_stock_imports(self):
        graph = self.graph(fixture(), "parcel")
        run = lib.node_by_output(graph, "$(B)/parcel/parcel.bin")
        self.assertEqual(run["cmds"][0]["cmd_args"], [
            "$(B)/tools/packer/packer", "--emit", "$(B)/parcel/parcel.bin",
        ])
        self.assertEqual(run["outputs"], ["$(B)/parcel/parcel.bin"])
        self.assertTrue({
            f"$(S)/{STOCK}/google/protobuf/parcel.proto",
            f"$(S)/{STOCK}/google/protobuf/leaf.proto",
            "$(S)/headers/detail.h",
        }.issubset(run["inputs"]))

    def test_final_include_switch_and_macro_environment(self):
        for body, enabled in (
            ("", False),
            ("RESOLVE_PROTO()\nSET(DONT_RESOLVE_INCLUDES yes)\n", False),
            ("RESOLVE_PROTO()\nSET(DONT_RESOLVE_INCLUDES)\n", False),
            ("RESOLVE_PROTO()\nSET(DONT_RESOLVE_INCLUDES no)\n", True),
            ("SET(DONT_RESOLVE_INCLUDES yes)\nRESOLVE_PROTO()\n", True),
        ):
            with self.subTest(body=body):
                files = fixture(body)
                if enabled:
                    files["parcel/ya.make"] = files["parcel/ya.make"].replace(
                        "RUN_PROGRAM(",
                        'IF(DONT_RESOLVE_INCLUDES != "no")\n'
                        "MESSAGE(FATAL_ERROR macro_did_not_set_no)\nENDIF()\nRUN_PROGRAM(",
                    )
                graph = self.graph(files, "parcel")
                inputs = lib.node_by_output(graph, "$(B)/parcel/parcel.bin")["inputs"]
                self.assertEqual(enabled, "$(S)/headers/detail.h" in inputs)
                self.assertEqual(enabled, f"$(S)/{STOCK}/google/protobuf/leaf.proto" in inputs)

    def test_macro_matches_its_upstream_expansion(self):
        macro = self.graph(fixture(), "parcel")
        expanded = self.graph(fixture(
            "SET(DONT_RESOLVE_INCLUDES no)\n"
            f"ADDINCL(FOR proto ${{ARCADIA_ROOT}}/{STOCK})\n"
        ), "parcel")
        self.assertEqual(macro, expanded)

    def test_proto_paths_are_local_and_do_not_become_c_include_paths(self):
        files = fixture("RESOLVE_PROTO()\nADDINCL(FOR proto schemas)\n")
        files["parcel/catalog.gzt"] += 'import "custom.proto";\n'
        files["schemas/custom.proto"] = 'import "google/protobuf/leaf.proto";\n'
        files["schemas/detail.h"] = "#pragma once\n"
        graph = self.graph(files, "parcel")
        run = lib.node_by_output(graph, "$(B)/parcel/parcel.bin")
        self.assertIn("$(S)/schemas/custom.proto", run["inputs"])
        self.assertIn("$(S)/headers/detail.h", run["inputs"])
        self.assertNotIn("$(S)/schemas/detail.h", run["inputs"])

    def test_proto_family_command_inputs_use_proto_language_include_paths(self):
        for extension in ("proto", "gztproto", "cfgproto", "ev"):
            with self.subTest(extension=extension):
                files = fixture()
                name = f"catalog.{extension}"
                files[f"parcel/{name}"] = files.pop("parcel/catalog.gzt")
                files["parcel/ya.make"] = files["parcel/ya.make"].replace("catalog.gzt", name)
                graph = self.graph(files, "parcel")
                run = lib.node_by_output(graph, "$(B)/parcel/parcel.bin")
                self.assertIn(f"$(S)/{STOCK}/google/protobuf/leaf.proto", run["inputs"])

    def test_peer_include_directories_are_available_when_resolution_is_enabled(self):
        files = fixture("PEERDIR(schema_exports)\nRESOLVE_PROTO()\n")
        files["schema_exports/ya.make"] = (
            f"LIBRARY()\n{lib.BARE_MODULE}"
            "ADDINCL(GLOBAL shared_headers GLOBAL FOR proto shared_schemas)\nEND()\n"
        )
        files["shared_headers/peer_detail.h"] = "#pragma once\n"
        files["shared_schemas/peer.proto"] = "message Peer {}\n"
        files["parcel/bridge.h"] += "#include <peer_detail.h>\n"
        files["parcel/catalog.gzt"] += 'import "peer.proto";\n'
        graph = self.graph(files, "parcel")
        run = lib.node_by_output(graph, "$(B)/parcel/parcel.bin")
        self.assertIn("$(S)/shared_headers/peer_detail.h", run["inputs"])
        self.assertIn("$(S)/shared_schemas/peer.proto", run["inputs"])

    def test_repeated_macro_calls_work_in_a_native_module(self):
        files = fixture("RESOLVE_PROTO()\nRESOLVE_PROTO()\nSRCS(consume.cpp)\n", "LIBRARY()")
        files["parcel/consume.cpp"] = '#include "parcel/parcel.bin"\n'
        graph = self.graph(files, "parcel")
        run = lib.node_by_output(graph, "$(B)/parcel/parcel.bin")
        self.assertEqual(run["inputs"].count(f"$(S)/{STOCK}/google/protobuf/leaf.proto"), 1)

    def test_macro_rejects_arguments(self):
        result = lib.make_process(fixture("RESOLVE_PROTO(unexpected)\n"), "parcel")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("RESOLVE_PROTO expects no arguments", result.stderr)

    def test_macro_uses_the_include_variable_at_its_call(self):
        files = fixture(
            "SET(PROTOBUF_INCLUDE_PATH ${ARCADIA_ROOT}/schemas)\n"
            "RESOLVE_PROTO()\nSET(PROTOBUF_INCLUDE_PATH ${ARCADIA_ROOT}/later)\n"
        )
        files["schemas/google/protobuf/parcel.proto"] = "message Replacement {}\n"
        graph = self.graph(files, "parcel")
        inputs = lib.node_by_output(graph, "$(B)/parcel/parcel.bin")["inputs"]
        self.assertIn("$(S)/schemas/google/protobuf/parcel.proto", inputs)
        self.assertNotIn(f"$(S)/{STOCK}/google/protobuf/parcel.proto", inputs)

    def test_generated_import_keeps_its_registered_producer_and_dependencies(self):
        files = fixture("RESOLVE_PROTO()\n")
        files["parcel/ya.make"] = files["parcel/ya.make"].replace(
            "RUN_PROGRAM(",
            "RUN_PROGRAM(tools/packer IN seed.txt OUT_NOAUTO generated.gzt "
            "OUTPUT_INCLUDES parcel/imported.gzt)\nRUN_PROGRAM(",
            1,
        )
        files["parcel/catalog.gzt"] = 'import "parcel/generated.gzt";\n'
        files["parcel/imported.gzt"] = 'import "google/protobuf/leaf.proto";\n'
        files["parcel/seed.txt"] = "seed\n"
        graph = self.graph(files, "parcel")
        producer = lib.node_by_output(graph, "$(B)/parcel/generated.gzt")
        consumer = lib.node_by_output(graph, "$(B)/parcel/parcel.bin")
        self.assertIn(producer["uid"], consumer["deps"])
        self.assertIn("$(B)/parcel/generated.gzt", consumer["inputs"])
        self.assertIn("$(S)/parcel/imported.gzt", consumer["inputs"])
        self.assertIn(f"$(S)/{STOCK}/google/protobuf/leaf.proto", consumer["inputs"])

    def test_disabled_and_enabled_modules_share_lexical_source_without_cache_poisoning(self):
        # Both UNION roots retain CLI order; both consume parcel/catalog.gzt.
        for order in (("muted", "parcel"), ("parcel", "muted")):
            with self.subTest(order=order):
                files = fixture()
                files["muted/ya.make"] = (
                    "UNION()\nRUN_PROGRAM(tools/packer "
                    "IN ${ARCADIA_ROOT}/parcel/catalog.gzt OUT muted.bin)\nEND()\n"
                )
                graph = self.graph(files, *order)
                enabled = lib.node_by_output(graph, "$(B)/parcel/parcel.bin")
                disabled = lib.node_by_output(graph, "$(B)/muted/muted.bin")
                leaf = f"$(S)/{STOCK}/google/protobuf/leaf.proto"
                self.assertIn(leaf, enabled["inputs"])
                self.assertNotIn(leaf, disabled["inputs"])

    def test_disabled_resolution_preserves_explicit_generated_input_edges(self):
        files = fixture("")
        files["parcel/ya.make"] = files["parcel/ya.make"].replace(
            "RUN_PROGRAM(",
            "RUN_PROGRAM(tools/packer IN seed.h OUT_NOAUTO generated.h)\nRUN_PROGRAM(",
            1,
        ).replace("IN catalog.gzt bridge.h", "IN generated.h")
        files["parcel/seed.h"] = '#include "absent.h"\n'
        graph = self.graph(files, "parcel")
        producer = lib.node_by_output(graph, "$(B)/parcel/generated.h")
        consumer = lib.node_by_output(graph, "$(B)/parcel/parcel.bin")
        self.assertIn(producer["uid"], consumer["deps"])
        self.assertIn("$(B)/parcel/generated.h", consumer["inputs"])
        self.assertIn("$(S)/parcel/seed.h", producer["inputs"])
        self.assertFalse(any("absent.h" in node["inputs"] for node in graph["graph"]))

    def test_disabled_resolution_also_suppresses_rooted_lexical_imports(self):
        files = fixture("")
        files["parcel/catalog.gzt"] = f'import "$(S)/{STOCK}/google/protobuf/leaf.proto";\n'
        graph = self.graph(files, "parcel")
        run = lib.node_by_output(graph, "$(B)/parcel/parcel.bin")
        self.assertNotIn(f"$(S)/{STOCK}/google/protobuf/leaf.proto", run["inputs"])

    def test_noparse_inputs_and_plain_file_copies_remain_opaque(self):
        files = fixture()
        files["parcel/ya.make"] = files["parcel/ya.make"].replace(
            "IN catalog.gzt bridge.h", "IN_NOPARSE catalog.gzt bridge.h",
        ).replace("END()", "FILES(catalog.gzt)\nEND()")
        graph = self.graph(files, "parcel")
        for output in ("$(B)/parcel/catalog.gzt", "$(B)/parcel/parcel.bin"):
            node = lib.node_by_output(graph, output)
            self.assertNotIn(f"$(S)/{STOCK}/google/protobuf/leaf.proto", node["inputs"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
