"""Generated event schemas consume their producer's declared import facts."""
import unittest

import lib
import test_emit_ev


def fixture(mode="generated"):
    files = test_emit_ev.EmitEventTest().common_files()
    lib.tool_program(files, "tools/schema_seed", "schema_seed")
    for module, name, imports, peers in (
        ("maple/deep", "core", "", ""),
        ("maple/base", "base", 'import "maple/deep/core.proto";\n', "maple/deep"),
        ("maple/other", "other", "", ""),
    ):
        files[f"{module}/ya.make"] = (
            f"PROTO_LIBRARY()\nONLY_TAGS(CPP_PROTO)\n{lib.BARE_MODULE}"
            + (f"PEERDIR({peers})\n" if peers else "")
            + f"SRCS({name}.proto)\nEND()\n"
        )
        files[f"{module}/{name}.proto"] = 'syntax = "proto3";\n' + imports
    generation = (
        "RUN_PROGRAM(tools/schema_seed IN_NOPARSE seed.txt OUT_NOAUTO emitted.ev"
        + (" OUTPUT_INCLUDES maple/base/base.proto" if mode != "opaque" else "")
        + ")\n"
    )
    if mode == "source":
        generation = ""
        files["maple/events/emitted.ev"] = 'import "maple/base/base.proto";\n'
    files["maple/events/ya.make"] = (
        f"PROTO_LIBRARY()\nONLY_TAGS(CPP_PROTO)\n{lib.BARE_MODULE}"
        "PEERDIR(maple/base maple/other)\n" + generation + "SRCS(emitted.ev)\nEND()\n"
    )
    files["maple/events/seed.txt"] = 'import "maple/other/other.proto";\n'
    files["willow/reader/ya.make"] = (
        f"LIBRARY()\n{lib.BARE_MODULE}PEERDIR(maple/events)\nSRCS(read.cpp)\nEND()\n"
    )
    files["willow/reader/read.cpp"] = '#include "maple/events/emitted.ev.pb.h"\n'
    files["tools/event2cpp/ya.make"] = files["tools/event2cpp/ya.make"].replace(
        "END()", "INDUCED_DEPS(h runtime/header.h)\nINDUCED_DEPS(cpp runtime/compile.h)\nINDUCED_DEPS(h+cpp runtime/common.h)\nEND()"
    )
    for name in ("header", "compile", "common"):
        files[f"runtime/{name}.h"] = "#pragma once\n"
    return files


class GeneratedEventHeadersTest(unittest.TestCase):
    def check_graph(self, graph, imported=True):
        event = lib.node_by_output(graph, "$(B)/maple/events/emitted.ev.pb.cc")
        self.assertEqual(event["outputs"], ["$(B)/maple/events/emitted.ev.pb.cc", "$(B)/maple/events/emitted.ev.pb.h"])
        for output, wanted, excluded in (
            ("maple/events/emitted.ev.pb.cc.o", "compile", "header"),
            ("willow/reader/read.cpp.o", "header", "compile"),
        ):
            cc = lib.node_by_output(graph, "$(B)/" + output)
            self.assertIn(event["uid"], cc["deps"])
            for path in ("maple/base/base.pb.h", "maple/deep/core.pb.h"):
                self.assertEqual("$(B)/" + path in cc["inputs"], imported)
                if imported:
                    self.assertIn(lib.node_by_output(graph, "$(B)/" + path)["uid"], cc["deps"])
            self.assertNotIn("$(B)/maple/other/other.pb.h", cc["inputs"])
            self.assertIn("$(S)/runtime/" + wanted + ".h", cc["inputs"])
            self.assertIn("$(S)/runtime/common.h", cc["inputs"])
            self.assertNotIn("$(S)/runtime/" + excluded + ".h", cc["inputs"])
            self.assertNotIn("$(S)/maple/events/seed.txt", cc["inputs"])
            self.assertNotIn("$(S)/build/scripts/cpp_proto_wrapper.py", cc["inputs"])

    def test_generated_event_imports_materialize_real_transitive_header_producers(self):
        graph = lib.make(fixture(), "willow/reader", "maple/events")
        self.check_graph(graph)
        seed = lib.node_by_output(graph, "$(B)/maple/events/emitted.ev")
        event = lib.node_by_output(graph, "$(B)/maple/events/emitted.ev.pb.cc")
        self.assertIn(seed["uid"], event["deps"])
        self.assertIn("$(B)/maple/events/emitted.ev", event["inputs"])

    def test_opaque_seed_text_does_not_create_imports(self):
        self.check_graph(lib.make(fixture("opaque"), "willow/reader", "maple/events"), imported=False)

    def test_physical_event_preserves_imports(self):
        self.check_graph(lib.make(fixture("source"), "willow/reader", "maple/events"))

    def test_generated_event_builtin_import_uses_protobuf_runtime_header(self):
        files = fixture()
        files["maple/events/ya.make"] = files["maple/events/ya.make"].replace(
            "OUTPUT_INCLUDES maple/base/base.proto",
            "OUTPUT_INCLUDES maple/base/base.proto google/protobuf/descriptor.proto",
        )
        header = "contrib/libs/protobuf/src/google/protobuf/descriptor.pb.h"
        files[header] = "#pragma once\n"
        graph = lib.make(files, "willow/reader", "maple/events")
        self.check_graph(graph)
        for output in ("maple/events/emitted.ev.pb.cc.o", "willow/reader/read.cpp.o"):
            self.assertIn("$(S)/" + header, lib.node_by_output(graph, "$(B)/" + output)["inputs"])

    def test_separate_generated_events_keep_independent_import_facts(self):
        files = fixture()
        files["maple/events/ya.make"] = files["maple/events/ya.make"].replace(
            "SRCS(emitted.ev)", "RUN_PROGRAM(tools/schema_seed IN_NOPARSE seed.txt OUT_NOAUTO separate.ev OUTPUT_INCLUDES maple/other/other.proto)\nSRCS(emitted.ev separate.ev)"
        )
        graph = lib.make(files, "willow/reader", "maple/events")
        self.check_graph(graph)
        separate = lib.node_by_output(graph, "$(B)/maple/events/separate.ev.pb.cc.o")
        self.assertIn("$(B)/maple/other/other.pb.h", separate["inputs"])
        self.assertNotIn("$(B)/maple/base/base.pb.h", separate["inputs"])
        self.assertNotIn("$(B)/maple/deep/core.pb.h", separate["inputs"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
