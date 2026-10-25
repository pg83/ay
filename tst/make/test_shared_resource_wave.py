"""Shared producer ownership does not replace a module's source-action wave."""
import unittest

from tst import lib
from tst.make.test_resource_readiness import fixture, resource_order


def shared_fixture(producer="sandbox"):
    files, _ = fixture(producer=producer.removeprefix("multi-"))
    if producer.startswith("multi-"):
        files["mod/ya.make"] = files["mod/ya.make"].replace("OUT_NOAUTO data.dat", "OUT_NOAUTO spare.dat data.dat")
    files["mod/ya.make"] = files["mod/ya.make"].replace("PY_SRCS(a.py)", "PY_SRCS(a.py b.py)")
    files["mod/b.py"] = ""
    files["reader/ya.make"] = "PROGRAM()\n" + lib.BARE_MODULE + "PEERDIR(mod)\nEND()\n"
    return files


class SharedResourceWaveTest(unittest.TestCase):
    def test_each_selected_context_retains_its_original_source_action_wave(self):
        for producer in ("sandbox", "program", "python", "multi-sandbox", "multi-program"):
            for targets in (("reader", "caller"), ("caller", "reader")):
                with self.subTest(producer=producer, targets=targets):
                    graph = lib.make(shared_fixture(producer), *targets, "--sandboxing")
                    self.assertEqual(resource_order(graph, False), ["python", "payload"])
                    self.assertEqual(resource_order(graph, True), ["payload", "namespace", "python"])
                    payload = lib.node_by_output(graph, "$(B)/mod/data.dat")
                    for py3 in (False, True):
                        archive = lib.node_by_output(graph, "$(B)/mod/libpy" + ("3" if py3 else "") + "mod.global.a")
                        nodes = {n["uid"]: n for n in graph["graph"]}
                        resources = [nodes[uid] for uid in archive["deps"]]
                        dependent = [n for n in resources if "$(B)/mod/data.dat" in n["inputs"]]
                        self.assertEqual(len(dependent), 1)
                        self.assertIn(payload["uid"], dependent[0]["deps"])

    def test_repeated_sandbox_output_in_one_context_is_rejected(self):
        files = shared_fixture()
        files["mod/ya.make"] = files["mod/ya.make"].replace("FROM_SANDBOX(FILE 42 OUT_NOAUTO data.dat)", "FROM_SANDBOX(FILE 42 OUT_NOAUTO data.dat)\nFROM_SANDBOX(FILE 42 OUT_NOAUTO data.dat)")
        result = lib.make_process(files, "reader", "caller", "--sandboxing")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("duplicate producer", result.stderr)

    def test_foreign_peer_producer_stays_ready_without_a_local_declaration(self):
        files = shared_fixture()
        files["provider/ya.make"] = "LIBRARY()\n" + lib.BARE_MODULE + "FROM_SANDBOX(FILE 42 OUT_NOAUTO foreign.dat)\nEND()\n"
        files["mod/ya.make"] = files["mod/ya.make"].replace("RESOURCE(data.dat payload)", "PEERDIR(provider)\nRESOURCE(../provider/foreign.dat payload)").replace("FROM_SANDBOX(FILE 42 OUT_NOAUTO data.dat)\n", "")
        for targets in (("reader", "caller"), ("caller", "reader")):
            graph = lib.make(files, *targets, "--sandboxing")
            self.assertEqual(resource_order(graph, False), ["payload", "python"])
            self.assertEqual(resource_order(graph, True), ["payload", "namespace", "python"])

    def test_prior_physical_or_early_producer_availability_is_preserved(self):
        for local, before in ((True, False), (False, True)):
            files = shared_fixture()
            if local:
                files["mod/data.dat"] = "physical"
            if before:
                files["mod/ya.make"] = files["mod/ya.make"].replace("RESOURCE(data.dat payload)\nFROM_SANDBOX(FILE 42 OUT_NOAUTO data.dat)", "FROM_SANDBOX(FILE 42 OUT_NOAUTO data.dat)\nRESOURCE(data.dat payload)")
            graph = lib.make(files, "reader", "caller", "--sandboxing")
            self.assertEqual(resource_order(graph, False), ["payload", "python"])
            self.assertEqual(resource_order(graph, True), ["payload", "namespace", "python"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
