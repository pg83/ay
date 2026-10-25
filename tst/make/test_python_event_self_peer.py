"""Event-only Python proto variants inherit the configured C++ self peer."""
import unittest

from tst import lib
from tst.make.test_proto_py2_context import proto_world
from tst.make.test_all_python_forwarding import complete_nodes


def fixture(setting="", sources="signal.ev", python2=False):
    files = proto_world()
    lib.tool_program(files, "tools/event2cpp", "event2cpp")
    files["maple/ya.make"] = "PROTO_LIBRARY()\n" + lib.BARE_MODULE + "NO_MYPY()\nPEERDIR(notes)\n" + setting + "\nSRCS(" + sources + ")\nEND()\n"
    for name in ("signal.ev", "signal.proto", "other.proto"):
        files["maple/" + name] = 'syntax="proto2";\nimport "notes/leaf.proto";\nmessage Item {}\n'
    files["notes/ya.make"] = "PROTO_LIBRARY()\n" + lib.BARE_MODULE + "NO_MYPY()\nADDINCL(GLOBAL FOR proto notes/schema_include)\nSRCS(leaf.proto)\nEND()\n"
    files["notes/leaf.proto"] = 'syntax="proto2";\nmessage Leaf {}\n'
    files["notes/schema_include/.keep"] = ""
    files["library/cpp/eventlog/proto/ya.make"] = "PROTO_LIBRARY()\n" + lib.BARE_MODULE + "NO_MYPY()\nEND()\n"
    files["library/cpp/eventlog/ya.make"] = "LIBRARY()\n" + lib.BARE_MODULE + "PEERDIR(library/cpp/maple_codec)\nADDINCL(GLOBAL FOR proto library/cpp/eventlog/schema_include)\nSRCS(event.cpp)\nEND()\n"
    files["library/cpp/eventlog/event.cpp"] = "int event_runtime;\n"
    files["library/cpp/eventlog/schema_include/.keep"] = ""
    files["library/cpp/maple_codec/ya.make"] = "LIBRARY()\n" + lib.BARE_MODULE + "ADDINCL(GLOBAL library/cpp/maple_codec/include)\nSRCS(codec.cpp)\nEND()\n"
    files["library/cpp/maple_codec/codec.cpp"] = "int codec;\n"
    files["library/cpp/maple_codec/include/.keep"] = ""
    files["reader/ya.make"] = ("PY2_PROGRAM()\n" if python2 else "PY3_PROGRAM()\n") + lib.BARE_MODULE + "NO_CHECK_IMPORTS()\nPEERDIR(maple)\nEND()\n"
    return files




class PythonEventSelfPeerTest(unittest.TestCase):
    def test_default_matches_explicit_optimization_for_all_schema_kinds(self):
        for python2 in (False, True):
            for sources in ("", "signal.ev", "signal.proto", "signal.ev other.proto", "signal.proto signal.ev", "signal.ev signal.proto"):
                with self.subTest(python2=python2, sources=sources):
                    actual = lib.make(fixture(sources=sources, python2=python2), "reader", "--sandboxing")
                    explicit = lib.make(fixture("OPTIMIZE_PY_PROTOS()", sources, python2), "reader", "--sandboxing")
                    self.assertEqual(complete_nodes(actual), complete_nodes(explicit))
                    self.assertEqual(actual["result"], explicit["result"])
                    compiled = [n for n in actual["graph"] if any(o.startswith("$(B)/maple/") and o.endswith("_raw.auxcpp.py" + ("2" if python2 else "3") + ".o") for o in n["outputs"])]
                    if ".ev" in sources:
                        self.assertTrue(compiled)
                        for node in compiled:
                            self.assertIn("-I$(S)/library/cpp/maple_codec/include", [a for c in node["cmds"] for a in c["cmd_args"]])
                        pb = lib.node_by_output(actual, "$(B)/maple/signal" + ("__intpy2___ev_pb2.py" if python2 else "__intpy3___ev_pb2.py"))
                        self.assertIn("$(S)/notes/leaf.proto", pb["inputs"])
                        self.assertIn("$(S)/maple/signal.ev", pb["inputs"])
                        compiled_event = lib.node_by_output(actual, "$(B)/maple/signal.ev.pb.cc.o")
                        self.assertIn("$(B)/notes/leaf.pb.h", compiled_event["inputs"])
                        self.assertIn(lib.node_by_output(actual, "$(B)/notes/leaf.pb.h")["uid"], compiled_event["deps"])
                        self.assertIn(lib.node_by_output(actual, "$(B)/maple/signal.ev.pb.h")["uid"], compiled_event["deps"])

    def test_ignore_cpp_self_peer_uses_exact_yes_for_source_and_cli_bindings(self):
        for sources in ("", "signal.proto", "signal.ev"):
            for value in ("yes", "no"):
                with self.subTest(sources=sources, value=value):
                    source = lib.make(fixture("SET(_IGNORE_CPP_PROTO_SELF_PEERS " + value + ")", sources), "reader", "--sandboxing")
                    cli = lib.make(fixture(sources=sources), "reader", "--sandboxing", "-D_IGNORE_CPP_PROTO_SELF_PEERS=" + value)
                    source_outputs = {o for n in source["graph"] for o in n["outputs"]}
                    cli_outputs = {o for n in cli["graph"] for o in n["outputs"]}
                    self.assertIn("$(B)/notes/leaf.pb.h", source_outputs)
                    self.assertEqual("$(B)/notes/leaf.pb.h" in cli_outputs, value == "no")
                    if sources:
                        suffix = "signal.ev.pb.h" if sources.endswith(".ev") else "signal.pb.h"
                        for graph, outputs in ((source, source_outputs), (cli, cli_outputs)):
                            self.assertEqual("$(B)/maple/" + suffix in outputs, value == "no")
                            # Ignoring the self peer doesn't change the unconditional value.
                            args = [a for n in graph["graph"] if "$(B)/reader/reader" in n["outputs"] for c in n["cmds"] for a in c["cmd_args"]]
                            self.assertIn("maple/libmaple.a", args)

    def test_explicitly_disabled_and_excluded_cpp_self_peer_stay_disabled(self):
        for setting in ("NO_OPTIMIZE_PY_PROTOS()", "EXCLUDE_TAGS(CPP_PROTO)"):
            with self.subTest(setting=setting):
                graph = lib.make(fixture(setting), "reader", "--sandboxing")
                outputs = {o for n in graph["graph"] for o in n["outputs"]}
                self.assertNotIn("$(B)/maple/signal.ev.pb.h", outputs)
                resource = next(n for n in graph["graph"] if any(o.startswith("$(B)/maple/") and o.endswith("_raw.auxcpp.py3.o") for o in n["outputs"]))
                self.assertNotIn("-I$(S)/library/cpp/maple_codec/include", [a for c in resource["cmds"] for a in c["cmd_args"]])


if __name__ == "__main__":
    unittest.main(verbosity=2)
