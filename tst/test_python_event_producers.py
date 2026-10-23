"""Python event schemas use the event generator identity and ordinary source channels."""
import unittest

import lib
from test_proto_py2_context import proto_world


def files(mode="source", python2=False, direct=False):
    result = proto_world()
    lib.tool_program(result, "tools/seed", "seed")
    tag = "PY_PROTO" if python2 else "PY3_PROTO"
    declaration = "RUN_PROGRAM(tools/seed OUT_NOAUTO signal.ev OUTPUT_INCLUDES maple/import.proto)\n" if mode != "source" else ""
    token = "signal.ev"
    if mode == "build":
        token = "${ARCADIA_BUILD_ROOT}/maple/signal.ev"
    if mode == "source-explicit":
        token = "${ARCADIA_ROOT}/maple/signal.ev"
    result["maple/ya.make"] = ("PY2_LIBRARY()\n" if python2 else "PY3_LIBRARY()\n") if direct else f"PROTO_LIBRARY()\nONLY_TAGS({tag})\nNO_OPTIMIZE_PY_PROTOS()\n"
    result["maple/ya.make"] += lib.BARE_MODULE + "PY_NAMESPACE(api.signals)\n" + declaration + ("PY_SRCS(" if direct else "SRCS(") + token + " plain.proto)\nEND()\n"
    result["maple/plain.proto"] = "message P {}\n"
    result["maple/import.proto"] = "message I {}\n"
    if mode in ("source", "source-explicit", "build", "shadow"):
        result["maple/signal.ev"] = 'import "maple/import.proto";\nmessage E {}\n'
    result["library/cpp/eventlog/proto/ya.make"] = f"PROTO_LIBRARY()\nONLY_TAGS({tag})\n{lib.BARE_MODULE}NO_OPTIMIZE_PY_PROTOS()\nNO_MYPY()\nEND()\n"
    result["reader/ya.make"] = ("PY2_PROGRAM()\n" if python2 else "PY3_PROGRAM()\n") + lib.BARE_MODULE + "NO_CHECK_IMPORTS()\nPEERDIR(maple)\nEND()\n"
    return result


class PythonEventProducersTest(unittest.TestCase):
    def test_event_and_proto_producers_have_independent_suffix_and_plugin_plans(self):
        for python2 in (False, True):
            for mode in ("source", "generated", "shadow", "build", "source-explicit"):
                with self.subTest(python2=python2, mode=mode):
                    graph = lib.make(files(mode, python2), "reader")
                    version = "py2" if python2 else "py3"
                    out = f"$(B)/maple/signal__int{version}___ev_pb2.py"
                    pb = lib.node_by_output(graph, out)
                    args = pb["cmds"][0]["cmd_args"]
                    self.assertEqual(pb["outputs"], [out])
                    self.assertEqual(args[args.index("--suffixes") + 1], "_ev_pb2.py")
                    self.assertEqual(pb["kv"][f"ext_out_name_for_signal__int{version}___ev_pb2.py"], "signal_ev_pb2.py")
                    self.assertFalse(any("mypy" in arg or "grpc" in arg for arg in args))
                    root = "$(S)" if mode in ("source", "shadow", "source-explicit") else "$(B)"
                    self.assertIn(root + "/maple/signal.ev", pb["inputs"])
                    self.assertIn("$(S)/maple/import.proto", pb["inputs"])
                    self.assertEqual(pb["cmds"][0]["cwd"], root)
                    if root == "$(B)":
                        self.assertIn(lib.node_by_output(graph, "$(B)/maple/signal.ev")["uid"], pb["deps"])
                    plain = lib.node_by_output(graph, f"$(B)/maple/plain__int{version}___pb2.py")
                    self.assertTrue(any("mypy" in arg for arg in plain["cmds"][0]["cmd_args"]))
                    bytecode = next(n for n in graph["graph"] if any(o.startswith(out + ".") and ".yapyc" in o for o in n["outputs"]))
                    self.assertIn(pb["uid"], bytecode["deps"])
                    resource_name = "api.signals.signal_ev_pb2" if python2 else "api/signals/signal_ev_pb2.py"
                    if mode in ("build", "source-explicit"):
                        prefix = "$(B)" if mode == "build" else "$(S)"
                        resource_name = "api.signals." + prefix + ".maple.signal_ev_pb2" if python2 else "api/signals/" + prefix + "/maple/signal_ev_pb2.py"
                    resources = [n for n in graph["graph"] if any(resource_name in arg for cmd in n["cmds"] for arg in cmd["cmd_args"])]
                    self.assertTrue(resources)
                    self.assertFalse(any("signal.ev" in o and "__int" not in o and o.endswith(".py") for n in graph["graph"] for o in n["outputs"]))

    def test_direct_python_sources_dispatch_events_through_the_proto_registry(self):
        for python2 in (False, True):
            with self.subTest(python2=python2):
                graph = lib.make(files(python2=python2, direct=True), "reader")
                version = "py2" if python2 else "py3"
                self.assertIn("$(S)/maple/signal.ev", lib.node_by_output(graph, f"$(B)/maple/signal__int{version}___ev_pb2.py")["inputs"])

    def test_event_plan_is_reused_without_inheriting_ordinary_proto_plugins(self):
        sources = files()
        sources["maple/ya.make"] = sources["maple/ya.make"].replace(
            "SRCS(signal.ev plain.proto)", "GRPC()\nSRCS(plain.proto signal.ev second.ev)")
        sources["maple/second.ev"] = "message Second {}\n"
        graph = lib.make(sources, "reader")
        for name in ("signal", "second"):
            event = lib.node_by_output(graph, f"$(B)/maple/{name}__intpy3___ev_pb2.py")
            self.assertEqual(len(event["outputs"]), 1)
            self.assertFalse(any("grpc" in arg or "mypy" in arg for arg in event["cmds"][0]["cmd_args"]))
        self.assertIn("$(B)/maple/plain__intpy3___pb2_grpc.py", lib.node_by_output(graph, "$(B)/maple/plain__intpy3___pb2.py")["outputs"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
