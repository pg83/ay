"""Descriptor collection follows selected event producers, including generated inputs."""
import hashlib
import unittest

import lib
from test_generated_event_headers import fixture
from test_proto_desc import desc_files


class EventDescriptorCollectionTest(unittest.TestCase):
    def test_selected_events_enter_both_descriptor_collections(self):
        for mode in ("source", "generated", "opaque", "source-explicit", "build-explicit"):
            with self.subTest(mode=mode):
                files = fixture("source" if mode in ("source", "source-explicit") else "opaque" if mode == "opaque" else "generated")
                files.update(desc_files())
                files["maple/events/ya.make"] = files["maple/events/ya.make"].replace(
                    "ONLY_TAGS(CPP_PROTO)", "ONLY_TAGS(CPP_PROTO DESC_PROTO)")
                files["maple/events/ya.make"] = files["maple/events/ya.make"].replace(
                    "SRCS(emitted.ev)", "RUN_PROGRAM(tools/schema_seed OUT_NOAUTO unused.ev)\nSRCS(emitted.ev)")
                if mode in ("source-explicit", "build-explicit"):
                    token = "${ARCADIA_BUILD_ROOT}/maple/events/emitted.ev" if mode == "build-explicit" else "${ARCADIA_ROOT}/maple/events/emitted.ev"
                    files["maple/events/ya.make"] = files["maple/events/ya.make"].replace("SRCS(emitted.ev)", "SRCS(" + token + ")")
                graph = lib.make(files, "maple/events")
                stem = "maple/events/emitted.ev"
                desc = lib.node_by_output(graph, "$(B)/" + stem + ".desc")
                raw = "$(B)/" + stem + "." + hashlib.md5(b"maple/events").hexdigest() + ".rawproto"
                self.assertEqual(desc["outputs"], ["$(B)/" + stem + ".desc", raw])
                root = "$(S)" if mode in ("source", "source-explicit") else "$(B)"
                self.assertIn(root + "/" + stem, desc["inputs"])
                self.assertEqual(desc["cmds"][0]["cwd"], root)
                if root == "$(B)":
                    self.assertIn(lib.node_by_output(graph, "$(B)/" + stem)["uid"], desc["deps"])
                merge = lib.node_by_output(graph, "$(B)/maple/events/maple-events.self.protodesc")
                self.assertIn(desc["outputs"][0], merge["cmds"][0]["cmd_args"])
                self.assertIn(raw.removeprefix("$(B)/"), merge["cmds"][1]["cmd_args"])
                self.assertIn(desc["uid"], merge["deps"])
                self.assertEqual(merge["cmds"][0]["cmd_args"].count(desc["outputs"][0]), 1)
                self.assertIn(raw, merge["inputs"])
                self.assertNotIn("$(S)/maple/other/other.proto", desc["inputs"])
                self.assertFalse(any("unused.ev" in output for node in graph["graph"] for output in node["outputs"]))

    def test_descriptor_only_direct_event_does_not_require_cpp_selection(self):
        files = fixture("source")
        files.update(desc_files())
        files["maple/events/ya.make"] = files["maple/events/ya.make"].replace(
            "ONLY_TAGS(CPP_PROTO)", "ONLY_TAGS(DESC_PROTO)")
        graph = lib.make(files, "maple/events")
        merge = lib.node_by_output(graph, "$(B)/maple/events/maple-events.self.protodesc")
        self.assertIn(lib.node_by_output(graph, "$(B)/maple/events/emitted.ev.desc")["uid"], merge["deps"])
        self.assertFalse(any("emitted.ev.pb.cc" in output for node in graph["graph"] for output in node["outputs"]))
        self.assertEqual(len(merge["cmds"][0]["cmd_args"]), 4)


if __name__ == "__main__":
    unittest.main(verbosity=2)
