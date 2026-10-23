"""Python proto value declarations survive source-free bridge modules."""
import unittest

import lib
from test_proto_py2_context import proto_world
from test_link_proto_peers import link_args
from test_whole_archive_peer_order import whole


def fixture(peers="bridge absent named ordinary", kind="PY3_PROGRAM", explicit=False):
    files = proto_world()
    for name, head, body in (
        ("seed", "PROTO_LIBRARY()", "SRCS(note.proto)"),
        ("bridge", "PROTO_LIBRARY()", "PEERDIR(seed)"),
        ("absent", "PROTO_LIBRARY()", "EXCLUDE_TAGS(CPP_PROTO)"),
        ("named", "PROTO_LIBRARY(special)", "NO_OPTIMIZE_PY_PROTOS()"),
        ("ordinary", "LIBRARY()", ""),
        ("west", "PY3_LIBRARY()", "PEERDIR(bridge absent)"),
        ("east", "PY3_LIBRARY()", "PEERDIR(absent named bridge)"),
    ):
        files[name + "/ya.make"] = head + "\n" + lib.BARE_MODULE + "NO_MYPY()\n" + body + "\nEND()\n"
    files["seed/note.proto"] = 'syntax="proto2";\nmessage Note {}\n'
    files["reader/ya.make"] = kind + "()\n" + lib.BARE_MODULE + "NO_CHECK_IMPORTS()\nPEERDIR(" + peers + ")\n" + ("WHOLE_ARCHIVE(ordinary)\n" if explicit else "") + "END()\n"
    if kind == "PROGRAM":
        files["reader/ya.make"] = files["reader/ya.make"].replace("END()", "SRCS(main.cpp)\nEND()")
        files["reader/main.cpp"] = "int main() { return 0; }\n"
    return files


class EmptyProtoWholeArchiveTest(unittest.TestCase):
    def test_source_free_python_proto_modules_publish_unconditional_values(self):
        for kind in ("PY2_PROGRAM", "PY3_PROGRAM"):
            with self.subTest(kind=kind):
                graph = lib.make(fixture(kind=kind), "reader", "--sandboxing")
                self.assertEqual(whole(link_args(graph, "$(B)/reader/reader")),
                                 ["seed/libseed.a", "bridge/libbridge.a", "absent/libabsent.a", "named/libspecial.a"])
                linked = lib.node_by_output(graph, "$(B)/reader/reader")
                outputs = {o for n in graph["graph"] for o in n["outputs"]}
                for path in ("bridge/libbridge.a", "absent/libabsent.a", "named/libspecial.a", "ordinary/libordinary.a"):
                    self.assertNotIn("$(B)/" + path, outputs)
                    self.assertNotIn("$(B)/" + path, linked["inputs"])
                seed = lib.node_by_output(graph, "$(B)/seed/libseed.a")
                self.assertIn(seed["uid"], linked["deps"])
                self.assertIn(seed["outputs"][0], linked["inputs"])
                generated = lib.node_by_output(graph, "$(B)/seed/note.pb.h")
                self.assertIn("$(S)/seed/note.proto", generated["inputs"])

    def test_empty_bridge_values_obey_existing_postfix_diamond_order(self):
        for peers, expected in (
            ("west east", ["seed", "bridge", "absent", "named"]),
            ("east west", ["absent", "named", "seed", "bridge"]),
        ):
            with self.subTest(peers=peers):
                graph = lib.make(fixture(peers), "reader", "--sandboxing")
                self.assertEqual(whole(link_args(graph, "$(B)/reader/reader")),
                                 [name + "/lib" + ("special" if name == "named" else name) + ".a" for name in expected])

    def test_cpp_consumers_and_empty_ordinary_libraries_do_not_declare_values(self):
        for peers in ("bridge named ordinary", "ordinary"):
            with self.subTest(peers=peers):
                graph = lib.make(fixture(peers, "PROGRAM"), "reader", "--sandboxing")
                self.assertEqual(whole(link_args(graph, "$(B)/reader/reader")), [])


if __name__ == "__main__":
    unittest.main(verbosity=2)
