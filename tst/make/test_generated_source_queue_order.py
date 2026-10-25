"""Initial source actions keep their FIFO wave independently of input closure."""
import unittest

from tst import lib
from tst.make.test_proto_cpp import proto_files


def world(shadow=True, before=True, root="relative", imported=True, src=False):
    files = proto_files()
    lib.tool_program(files, "tools/carver", "carver")
    prefix = {"relative": "", "source": "${CURDIR}/", "build": "${BINDIR}/"}[root]
    declaration = (
        "RUN_PROGRAM(tools/carver OUT_NOAUTO first.proto second.proto "
        "OUTPUT_INCLUDES clay/first.proto)\n"
    )
    listed = (
        f"SRC({prefix}first.proto)\nSRC({prefix}second.proto)\nSRCS(ordinary.proto local.cpp)\n"
        if src else f"SRCS({prefix}first.proto {prefix}second.proto ordinary.proto local.cpp)\n"
    )
    files["clay/ya.make"] = (
        f"PROTO_LIBRARY()\nONLY_TAGS(CPP_PROTO)\n{lib.BARE_MODULE}"
        + (declaration + listed if before else listed + declaration) + "END()\n"
    )
    files["clay/ordinary.proto"] = 'syntax = "proto3";\n'
    files["clay/local.cpp"] = "int local;\n"
    if shadow:
        files["clay/first.proto"] = 'syntax = "proto3";\n'
        files["clay/second.proto"] = 'syntax = "proto3";\n' + (
            'import "clay/first.proto";\n' if imported else ""
        )
    return files


class GeneratedSourceQueueOrderTest(unittest.TestCase):
    def check_archive(self, graph, order):
        ar = lib.node_by_output(graph, "$(B)/clay/libclay.a")
        outputs = [f"$(B)/clay/{name}" for name in order]
        self.assertEqual(ar["cmds"][0]["cmd_args"][10:], outputs)
        self.assertEqual({p for p in ar["inputs"] if p.endswith(".o")}, set(outputs))
        for output in outputs:
            producer = lib.node_by_output(graph, output)
            self.assertIn(producer["uid"], ar["deps"])
            self.assertIn(output, ar["inputs"])
        return ar

    def check_producers(self, graph, source_root):
        for name in ("first", "second", "ordinary"):
            pb = lib.node_by_output(graph, f"$(B)/clay/{name}.pb.h")
            cc = lib.node_by_output(graph, f"$(B)/clay/{name}.pb.cc.o")
            root = "$(S)" if name == "ordinary" else source_root
            self.assertIn(f"{root}/clay/{name}.proto", pb["inputs"])
            self.assertEqual(pb["cmds"][0]["cwd"], root)
            self.assertEqual(pb["outputs"], [f"$(B)/clay/{name}.pb.h", f"$(B)/clay/{name}.pb.cc"])
            self.assertIn(pb["uid"], cc["deps"])
            self.assertIn(pb["outputs"][0], cc["inputs"])
            protoc = lib.node_by_output(graph, "$(B)/contrib/tools/protoc/protoc")
            self.assertIn(protoc["uid"], pb["deps"])
            self.assertEqual(protoc["platform"], "default-linux-x86_64")
            self.assertEqual(pb["platform"], "default-linux-aarch64")
            if root == "$(B)":
                generated = lib.node_by_output(graph, "$(B)/clay/first.proto")
                self.assertEqual(generated["outputs"], ["$(B)/clay/first.proto", "$(B)/clay/second.proto"])
                self.assertIn(generated["uid"], pb["deps"])
                self.assertIn("$(B)/clay/first.proto", pb["inputs"])
                carver = lib.node_by_output(graph, "$(B)/tools/carver/carver")
                self.assertIn(carver["uid"], generated["deps"])
                self.assertIn(carver["outputs"][0], generated["inputs"])

    def test_initial_schema_roots_keep_order_for_source_and_generated_inputs(self):
        for shadow in (False, True):
            for before in (False, True):
                for root in ("relative", "source", "build"):
                    if root == "source" and not shadow:
                        continue
                    with self.subTest(shadow=shadow, before=before, root=root):
                        graph = lib.make(world(shadow, before, root), "clay")
                        self.check_archive(graph, ["local.cpp.o", "first.pb.cc.o", "second.pb.cc.o", "ordinary.pb.cc.o"])
                        self.check_producers(graph, "$(S)" if shadow and root != "build" else "$(B)")

    def test_unimported_source_shadows_remain_a_preservation_control(self):
        graph = lib.make(world(imported=False), "clay")
        self.check_archive(graph, ["local.cpp.o", "first.pb.cc.o", "second.pb.cc.o", "ordinary.pb.cc.o"])
        self.check_producers(graph, "$(S)")
        self.assertFalse(any("$(B)/clay/first.proto" in n["outputs"] for n in graph["graph"]))

    def test_graph_root_order_does_not_change_the_initial_source_wave(self):
        files = world()
        files["other/ya.make"] = f"LIBRARY()\n{lib.BARE_MODULE}SRCS(other.cpp)\nEND()\n"
        files["other/other.cpp"] = "int other;\n"
        for roots in (("other", "clay"), ("clay", "other")):
            with self.subTest(roots=roots):
                graph = lib.make(files, *roots)
                self.check_archive(graph, ["local.cpp.o", "first.pb.cc.o", "second.pb.cc.o", "ordinary.pb.cc.o"])
                self.check_producers(graph, "$(S)")

    def test_priority_two_src_before_producer_remains_pending(self):
        graph = lib.make(world(shadow=False, before=False, root="build", src=True), "clay")
        self.check_archive(graph, ["local.cpp.o", "ordinary.pb.cc.o", "first.pb.cc.o", "second.pb.cc.o"])
        self.check_producers(graph, "$(B)")

    def test_auto_source_of_an_initial_schema_adds_a_real_queue_wave(self):
        files = world()
        lib.tool_program(files, "dict/gazetteer/converter", "converter")
        files["kernel/gazetteer/proto/ya.make"] = f"LIBRARY()\n{lib.BARE_MODULE}END()\n"
        files["clay/raw.gztproto"] = "message Raw {}\n"
        files["clay/ya.make"] = (
            f"PROTO_LIBRARY()\nONLY_TAGS(CPP_PROTO)\n{lib.BARE_MODULE}"
            "SRCS(raw.gztproto ordinary.proto local.cpp)\nEND()\n"
        )
        graph = lib.make(files, "clay")
        self.check_archive(graph, ["local.cpp.o", "ordinary.pb.cc.o", "raw.pb.cc.o"])
        converted = lib.node_by_output(graph, "$(B)/clay/raw.proto")
        pb = lib.node_by_output(graph, "$(B)/clay/raw.pb.h")
        self.assertIn("$(S)/clay/raw.gztproto", converted["inputs"])
        self.assertIn(converted["uid"], pb["deps"])
        self.assertIn(converted["outputs"][0], pb["inputs"])

    def test_host_and_target_archive_waves_use_distinct_producer_contexts(self):
        files = world(shadow=False)
        lib.tool_program(files, "tools/consumer", "consumer")
        files["tools/consumer/ya.make"] = files["tools/consumer/ya.make"].replace("END()", "PEERDIR(clay)\nEND()")
        files["caller/ya.make"] = (
            f"LIBRARY()\n{lib.BARE_MODULE}RUN_PROGRAM(tools/consumer OUT piece.cpp)\nEND()\n"
        )
        graph = lib.make(files, "caller", "clay")
        archives = [n for n in graph["graph"] if "$(B)/clay/libclay.a" in n["outputs"]]
        self.assertEqual({n["platform"] for n in archives}, {"default-linux-aarch64", "default-linux-x86_64"})
        for ar in archives:
            with self.subTest(platform=ar["platform"]):
                suffix = ".pic.o" if ar["platform"] == "default-linux-x86_64" else ".o"
                self.assertEqual(ar["cmds"][0]["cmd_args"][10:],
                                 ["$(B)/clay/" + p + suffix for p in ("local.cpp", "first.pb.cc", "second.pb.cc", "ordinary.pb.cc")])
                for path in ar["cmds"][0]["cmd_args"][10:]:
                    producer = next(n for n in graph["graph"] if path in n["outputs"] and n["platform"] == ar["platform"])
                    self.assertIn(producer["uid"], ar["deps"])
        tool = lib.node_by_output(graph, "$(B)/tools/consumer/consumer")
        host = next(n for n in archives if n["platform"] == "default-linux-x86_64")
        generated = lib.node_by_output(graph, "$(B)/caller/piece.cpp")
        self.assertIn(host["uid"], tool["deps"])
        self.assertIn(tool["uid"], generated["deps"])
        self.assertIn(tool["outputs"][0], generated["inputs"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
