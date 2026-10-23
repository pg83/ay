import unittest

import lib
from test_link_proto_peers import between, fixture as proto_fixture, link_args
from test_whole_archive_peer_order import whole


def fixture():
    files = proto_fixture()
    for name, body, sources in (
        ("foundation", "", "SRCS(note.proto)"),
        ("embedded", "PEERDIR(foundation)\n", "SRCS(GLOBAL note.proto)"),
        ("unoptimized", "NO_OPTIMIZE_PY_PROTOS()\nPEERDIR(foundation)\n", "SRCS(note.proto)"),
        ("absent", "EXCLUDE_TAGS(CPP_PROTO)\n", "SRCS(note.proto)"),
        ("named", "EXCLUDE_TAGS(CPP_PROTO)\n", "SRCS(note.proto)"),
    ):
        files[f"{name}/ya.make"] = (
            f"PROTO_LIBRARY({'custom' if name == 'named' else ''})\n"
            f"{lib.BARE_MODULE}{body}{sources}\nEND()\n"
        )
        files[f"{name}/note.proto"] = 'syntax = "proto3";\nmessage Note {}\n'
    for name, peers in (("west", "embedded unoptimized absent named"), ("east", "absent embedded")):
        files[f"{name}/ya.make"] = f"PY3_LIBRARY()\n{lib.BARE_MODULE}PEERDIR({peers})\nEND()\n"
    files["consumer/ya.make"] = f"PY3_PROGRAM()\n{lib.BARE_MODULE}PEERDIR(west east)\nEND()\n"
    files["reverse/ya.make"] = f"PY3_PROGRAM()\n{lib.BARE_MODULE}PEERDIR(east west)\nEND()\n"
    files["native/ya.make"] = f"PROGRAM()\n{lib.BARE_MODULE}PEERDIR(embedded foundation)\nSRCS(main.cpp)\nEND()\n"
    files["native/main.cpp"] = "int main() {}\n"
    return files


class WholeArchiveMembershipTest(unittest.TestCase):
    def check_consumer(self, graph, target, expected):
        self.assertTrue(graph["result"])
        args = link_args(graph, f"$(B)/{target}/{target}")
        self.assertEqual(whole(args), expected)
        self.assertEqual(len(whole(args)), len(set(whole(args))))
        node = lib.node_by_output(graph, f"$(B)/{target}/{target}")
        # The embedded proto has a C++ global archive, but no ordinary archive.
        # Its unconditional whole-archive value does not create a producer.
        outputs = {output for entry in graph["graph"] for output in entry["outputs"]}
        for path in ("embedded/libembedded.a", "unoptimized/libunoptimized.a", "absent/libabsent.a", "named/libcustom.a"):
            self.assertNotIn(f"$(B)/{path}", outputs)
            self.assertNotIn(f"$(B)/{path}", node["inputs"])
        for path in ("embedded/libembedded.global.a", "embedded/libpy3embedded.global.a", "foundation/libfoundation.a"):
            producer = lib.node_by_output(graph, f"$(B)/{path}")
            self.assertIn(producer["uid"], node["deps"])
            self.assertIn(f"$(B)/{path}", node["inputs"])
        compiled = lib.node_by_output(graph, "$(B)/embedded/note.pb.cc.o")
        embedded = lib.node_by_output(graph, "$(B)/embedded/libembedded.global.a")
        generated = lib.node_by_output(graph, "$(B)/embedded/note.pb.h")
        protoc = lib.node_by_output(graph, "$(B)/contrib/tools/protoc/protoc")
        self.assertIn(compiled["uid"], embedded["deps"])
        self.assertIn(generated["uid"], compiled["deps"])
        self.assertIn(protoc["uid"], generated["deps"])
        self.assertIn("$(S)/embedded/note.proto", generated["inputs"])
        self.assertEqual(compiled["platform"], node["platform"])
        self.assertEqual(protoc["platform"], "default-linux-x86_64")
        self.assertFalse(any("$(B)/unoptimized/note.pb.cc" in n["outputs"] for n in graph["graph"]))
        return args

    def test_global_only_and_disabled_siblings_publish_values_through_a_diamond(self):
        graph = lib.make(fixture(), "consumer")
        args = self.check_consumer(graph, "consumer", [
            "foundation/libfoundation.a", "embedded/libembedded.a", "unoptimized/libunoptimized.a",
            "absent/libabsent.a", "named/libcustom.a",
        ])
        self.assertEqual(between(args, "-Wl,--start-group", "-Wl,--end-group"), [
            "foundation/libfoundation.a", "contrib/python/protobuf/libcontrib-python-protobuf.a",
        ])
        self.assertEqual(between(args, "--ya-start-command-file", "--ya-end-command-file"), [
            "embedded/libembedded.global.a", "foundation/libpy3foundation.global.a",
            "embedded/libpy3embedded.global.a", "unoptimized/libpy3unoptimized.global.a",
            "absent/libpy3absent.global.a", "named/libpy3custom.global.a",
        ])

    def test_reversed_branches_and_extra_roots_keep_first_visit_membership(self):
        for roots in (("reverse",), ("east", "reverse"), ("reverse", "east")):
            with self.subTest(roots=roots):
                graph = lib.make(fixture(), *roots)
                self.check_consumer(graph, "reverse", [
                    "absent/libabsent.a", "foundation/libfoundation.a", "embedded/libembedded.a",
                    "unoptimized/libunoptimized.a", "named/libcustom.a",
                ])

    def test_native_consumers_keep_the_global_archive_without_python_values(self):
        graph = lib.make(fixture(), "native")
        args = link_args(graph, "$(B)/native/native")
        self.assertEqual(whole(args), [])
        self.assertEqual(between(args, "--ya-start-command-file", "--ya-end-command-file"), [
            "embedded/libembedded.global.a",
        ])
        self.assertEqual(between(args, "-Wl,--start-group", "-Wl,--end-group"), ["foundation/libfoundation.a"])
        node = lib.node_by_output(graph, "$(B)/native/native")
        producer = lib.node_by_output(graph, "$(B)/embedded/libembedded.global.a")
        self.assertIn(producer["uid"], node["deps"])
        self.assertIn("$(B)/embedded/libembedded.global.a", node["inputs"])
        self.assertNotIn("$(B)/embedded/libembedded.a", node["inputs"])

    def test_python2_and_python3_publish_the_same_values_with_distinct_resources(self):
        from test_proto_py2_context import proto_world

        files = proto_world()
        files["p/ya.make"] = f"PROTO_LIBRARY()\n{lib.BARE_MODULE}PEERDIR(q)\nSRCS(GLOBAL a.proto)\nEND()\n"
        files["q/ya.make"] = f"PROTO_LIBRARY()\n{lib.BARE_MODULE}NO_OPTIMIZE_PY_PROTOS()\nSRCS(b.proto)\nEND()\n"
        files["py3/ya.make"] = f"PY3_PROGRAM()\n{lib.BARE_MODULE}NO_CHECK_IMPORTS()\nPEERDIR(p)\nEND()\n"
        graph = lib.make(files, "py", "py3")
        for target, prefix in (("py", "libpy"), ("py3", "libpy3")):
            with self.subTest(target=target):
                args = link_args(graph, f"$(B)/{target}/{target}")
                self.assertEqual(whole(args), ["q/libq.a", "p/libp.a"])
                node = lib.node_by_output(graph, f"$(B)/{target}/{target}")
                self.assertNotIn("$(B)/p/libp.a", node["inputs"])
                # The C++ p context still selects its own ordinary q dependency.
                for archive in ("q/libq.a", "p/libp.global.a", f"p/{prefix}p.global.a", f"q/{prefix}q.global.a"):
                    producer = lib.node_by_output(graph, f"$(B)/{archive}")
                    self.assertIn(producer["uid"], node["deps"])
                    self.assertIn(f"$(B)/{archive}", node["inputs"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
