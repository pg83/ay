"""Schema kinds are packed by native Python callback, not one mixed batch."""
import unittest

from tst import lib
from tst.make.test_all_python_forwarding import complete_nodes
from tst.make.test_proto_py2_context import proto_world


def fixture(body, python2=False, direct=False):
    files = proto_world()
    tag = "PY_PROTO" if python2 else "PY3_PROTO"
    files["maple/ya.make"] = (("PY2_LIBRARY()\n" if python2 else "PY3_LIBRARY()\n") if direct else f"PROTO_LIBRARY()\nONLY_TAGS({tag})\nNO_OPTIMIZE_PY_PROTOS()\n") + lib.BARE_MODULE + "NO_MYPY()\n" + body + "\nEND()\n"
    for name in ("a.proto", "b.proto", "signal.ev", "other.ev", *(f"schema_{i:02}.proto" for i in range(12))):
        files["maple/" + name] = "message Item {}\n"
    files["library/cpp/eventlog/proto/ya.make"] = f"PROTO_LIBRARY()\nONLY_TAGS({tag})\n{lib.BARE_MODULE}NO_OPTIMIZE_PY_PROTOS()\nNO_MYPY()\nEND()\n"
    files["reader/ya.make"] = ("PY2_PROGRAM()\n" if python2 else "PY3_PROGRAM()\n") + lib.BARE_MODULE + "NO_CHECK_IMPORTS()\nPEERDIR(maple)\nEND()\n"
    return files


def producer_payloads(graph):
    identities = {n["uid"]: tuple(n["outputs"]) for n in graph["graph"]}
    nodes = complete_nodes(graph)
    for node in nodes.values():
        for field in ("uid", "self_uid"):
            node[field] = identities[node[field]]
        node["deps"] = [identities[dep] for dep in node["deps"]]
    return nodes


def resource_nodes(graph):
    return [n for n in graph["graph"] if any(o.startswith("$(B)/maple/") and o.endswith("_raw.auxcpp") for o in n["outputs"])]


class PythonEventResourceGroupsTest(unittest.TestCase):
    def paired(self, body, explicit, python2=False, direct=False):
        actual = lib.make(fixture(body, python2, direct), "reader")
        expected = lib.make(fixture(explicit, python2, direct), "reader")
        self.assertEqual(producer_payloads(actual), producer_payloads(expected))
        self.assertEqual(actual["result"], expected["result"])
        for n in resource_nodes(actual):
            args = [a for c in n["cmds"] for a in c["cmd_args"]]
            has_event = any("_ev_pb2" in a for a in args)
            has_proto = any("_pb2" in a and "_ev_pb2" not in a for a in args)
            self.assertFalse(has_event and has_proto)
        return actual

    def test_mixed_statement_and_reversed_order_match_separate_callbacks(self):
        for direct in (False, True):
            macro = "PY_SRCS" if direct else "SRCS"
            for sources in ("a.proto signal.ev b.proto", "signal.ev a.proto b.proto", "a.proto b.proto signal.ev"):
                with self.subTest(direct=direct, sources=sources):
                    self.paired(f"{macro}({sources})", f"{macro}(a.proto b.proto)\n{macro}(signal.ev)", direct=direct)

    def test_each_original_callback_keeps_its_own_proto_then_event_batches(self):
        self.paired("SRCS(a.proto signal.ev)\nSRCS(b.proto other.ev)", "SRCS(a.proto)\nSRCS(signal.ev)\nSRCS(b.proto)\nSRCS(other.ev)")

    def test_chunk_boundaries_are_independent_for_python2_and_python3(self):
        protos = " ".join(f"schema_{i:02}.proto" for i in range(12))
        mixed = " ".join([*(f"schema_{i:02}.proto" for i in range(5)), "signal.ev", *(f"schema_{i:02}.proto" for i in range(5,12)), "other.ev"])
        for python2 in (False, True):
            with self.subTest(python2=python2):
                graph = self.paired(f"SRCS({mixed})", f"SRCS({protos})\nSRCS(signal.ev other.ev)", python2)
                self.assertGreater(len(resource_nodes(graph)), 2)

    def test_ordinary_only_and_event_only_callbacks_stay_intact(self):
        for sources in ("a.proto b.proto", "signal.ev other.ev"):
            with self.subTest(sources=sources):
                graph = lib.make(fixture(f"SRCS({sources})"), "reader")
                self.assertEqual(len(resource_nodes(graph)), 1)
                for name in sources.split():
                    suffix = "_ev_pb2.py" if name.endswith(".ev") else "_pb2.py"
                    output = "$(B)/maple/" + name.rsplit(".",1)[0] + "__intpy3__" + suffix
                    producer = lib.node_by_output(graph, output)
                    self.assertEqual(producer["outputs"], [output])


if __name__ == "__main__":
    unittest.main(verbosity=2)
