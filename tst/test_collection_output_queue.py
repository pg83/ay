import json
import unittest

import lib


PREFIX = "$(B)/satchel/"


def fixture(body, opener="UNION(satchel)"):
    files = {
        "satchel/ya.make": opener + "\n" + body + "END()\n",
        "satchel/start.dat": "first physical data\n",
        "satchel/end.dat": "last physical data\n",
        "satchel/pattern.h.in": '#include "detail.h"\n',
        "satchel/detail.h": "// parsed dependency\n",
    }
    lib.tool_program(files, "tools/stamper", "stamper")
    return files


class CollectionOutputQueueTest(unittest.TestCase):
    def make(self, files, *targets):
        targets = targets or ("satchel",)
        return lib.make(files, targets[-1], *targets[:-1], "--sandboxing")

    def assert_union(self, graph, names, module="satchel"):
        base = "$(B)/" + module + "/"
        node = lib.node_by_output(graph, base + module + ".pkg.fake")
        self.assertEqual(node["outputs"], [base + module + ".pkg.fake", *[base + n for n in names]])
        self.assertEqual(node["cmds"][0]["cmd_args"][-2:], ["-c", "open('" + base + module + ".pkg.fake','w').close()"])
        for name in names:
            output = base + name
            producer = next(n for n in graph["graph"] if n is not node and output in n["outputs"]
                            and n["kv"].get("p") != "PK")
            self.assertIn(output, node["inputs"])
            self.assertIn(producer["uid"], node["deps"])
        return node

    def assert_copy(self, graph, source, destination, dependencies=(), extra_inputs=()):
        node = lib.node_by_output(graph, PREFIX + destination)
        self.assertEqual(node["outputs"], [PREFIX + destination])
        self.assertEqual(node["cmds"][0]["cmd_args"][1:],
                         ["$(S)/build/scripts/fs_tools.py", "copy", source, PREFIX + destination])
        self.assertEqual(set(node["inputs"]), {source, *extra_inputs})
        self.assertEqual(set(node["deps"]), set(dependencies))
        return node

    def test_ready_commands_keep_interleaved_declaration_order(self):
        for first in (True, False):
            with self.subTest(copy_first=first):
                copy = "FILES(start.dat)\n"
                sandbox = "FROM_SANDBOX(246 OUT middle.dat)\n"
                graph = self.make(fixture((copy + sandbox if first else sandbox + copy)
                                          + "COPY_FILE(end.dat installed.dat)\n"))
                expected = ["start.dat", "middle.dat"] if first else ["middle.dat", "start.dat"]
                self.assert_union(graph, [*expected, "installed.dat"])
                self.assert_copy(graph, "$(S)/satchel/start.dat", "start.dat")
                self.assert_copy(graph, "$(S)/satchel/end.dat", "installed.dat")

    def test_pending_copy_goes_to_tail_after_remaining_ready_commands(self):
        graph = self.make(fixture("COPY_FILE(${BINDIR}/future.dat consumed.dat)\nFILES(start.dat)\n"
            "FROM_SANDBOX(246 OUT future.dat secondary.dat)\nCOPY_FILE(end.dat installed.dat)\n"))
        self.assert_union(graph, ["start.dat", "future.dat", "secondary.dat", "installed.dat", "consumed.dat"])
        sb = lib.node_by_output(graph, PREFIX + "future.dat")
        self.assertEqual(sb["outputs"], [PREFIX + "future.dat", PREFIX + "secondary.dat"])
        self.assert_copy(graph, PREFIX + "future.dat", "consumed.dat", [sb["uid"]])

    def test_pending_chain_obeys_fifo_not_repeated_earliest_ready_selection(self):
        graph = self.make(fixture("COPY_FILE(${BINDIR}/future.dat consumed.dat)\n"
            "COPY_FILE(${BINDIR}/consumed.dat twice.dat)\nFILES(start.dat)\n"
            "FROM_SANDBOX(246 OUT future.dat secondary.dat)\nCOPY_FILE(end.dat installed.dat)\n"))
        self.assert_union(graph, ["start.dat", "future.dat", "secondary.dat", "installed.dat", "consumed.dat", "twice.dat"])
        first = lib.node_by_output(graph, PREFIX + "consumed.dat")
        sb = lib.node_by_output(graph, PREFIX + "future.dat")
        self.assert_copy(graph, PREFIX + "consumed.dat", "twice.dat", [first["uid"]])

    def test_pending_rounds_skip_completed_declarations(self):
        graph = self.make(fixture("COPY_FILE(${BINDIR}/middle.dat consumed.dat)\n"
            "COPY_FILE(${BINDIR}/future.dat middle.dat)\nFILES(start.dat)\nFROM_SANDBOX(246 OUT future.dat)\n"
            "COPY_FILE(${BINDIR}/consumed.dat installed.dat)\n"))
        self.assert_union(graph, ["start.dat", "future.dat", "middle.dat", "consumed.dat", "installed.dat"])
        for source, destination in (("future.dat", "middle.dat"), ("middle.dat", "consumed.dat"), ("consumed.dat", "installed.dat")):
            producer = lib.node_by_output(graph, PREFIX + source)
            self.assert_copy(graph, PREFIX + source, destination, [producer["uid"]])

    def test_main_secondary_and_unused_noauto_outputs_keep_producer_order(self):
        graph = self.make(fixture("FILES(start.dat)\n"
            "RUN_PROGRAM(tools/stamper --stamp IN end.dat OUT auto.dat auto2.dat OUT_NOAUTO opaque.dat opaque2.dat)\n"
            "FROM_SANDBOX(246 OUT remote.dat OUT_NOAUTO remote-secondary.dat)\nCOPY_FILE(end.dat installed.dat)\n"))
        self.assert_union(graph, ["start.dat", "auto.dat", "auto2.dat", "opaque.dat", "opaque2.dat",
                                  "remote.dat", "remote-secondary.dat", "installed.dat"])
        run = lib.node_by_output(graph, PREFIX + "auto.dat")
        tool = lib.node_by_output(graph, "$(B)/tools/stamper/stamper")
        self.assertEqual(run["outputs"], [PREFIX + n for n in ("auto.dat", "auto2.dat", "opaque.dat", "opaque2.dat")])
        self.assertEqual(set(run["inputs"]), {tool["outputs"][0], "$(S)/satchel/end.dat"})
        self.assertEqual(run["deps"], [tool["uid"]])
        self.assertEqual(run["cmds"][0]["cmd_args"], [tool["outputs"][0], "--stamp"])

    def test_secondary_output_readiness_keeps_mainoutput_and_original_inputs(self):
        graph = self.make(fixture("COPY_FILE(${BINDIR}/side.dat consumed.dat)\nFILES(start.dat)\n"
            "RUN_PROGRAM(tools/stamper IN end.dat OUT_NOAUTO main.dat side.dat)\n"
            "COPY_FILE(end.dat installed.dat)\n"))
        self.assert_union(graph, ["start.dat", "main.dat", "side.dat", "installed.dat", "consumed.dat"])
        run = lib.node_by_output(graph, PREFIX + "side.dat")
        self.assert_copy(graph, PREFIX + "side.dat", "consumed.dat", [run["uid"]],
                         [PREFIX + "main.dat"])

    def test_text_and_parsed_templates_retain_distinct_input_edges(self):
        graph = self.make(fixture("SET(DONT_RESOLVE_INCLUDES no)\n"
            "COPY_FILE(TEXT pattern.h.in text.h.in)\nFROM_SANDBOX(246 OUT middle.dat)\n"
            "COPY_FILE_WITH_CONTEXT(pattern.h.in parsed.h.in)\n"))
        self.assert_union(graph, ["text.h.in", "middle.dat", "parsed.h.in"])
        self.assert_copy(graph, "$(S)/satchel/pattern.h.in", "text.h.in")
        self.assert_copy(graph, "$(S)/satchel/pattern.h.in", "parsed.h.in", extra_inputs=["$(S)/satchel/detail.h"])

    def test_shared_lexical_include_has_distinct_module_queues(self):
        files = fixture("")
        files["shared/decls.inc"] = "FILES(start.dat)\nFROM_SANDBOX(246 OUT middle.dat)\nCOPY_FILE(end.dat installed.dat)\n"
        for module in ("satchel", "packet"):
            files[module + "/ya.make"] = "UNION(" + module + ")\nINCLUDE(${ARCADIA_ROOT}/shared/decls.inc)\nEND()\n"
            files[module + "/start.dat"] = module + " first\n"
            files[module + "/end.dat"] = module + " last\n"
        for order in (("satchel", "packet"), ("packet", "satchel")):
            with self.subTest(order=order):
                graph = self.make(files, *order)
                for module in order:
                    self.assert_union(graph, ["start.dat", "middle.dat", "installed.dat"], module)
                    cp = lib.node_by_output(graph, "$(B)/" + module + "/start.dat")
                    self.assertEqual(cp["inputs"], ["$(S)/" + module + "/start.dat"])

    def test_empty_union_remains_one_fake_output(self):
        graph = self.make(fixture(""))
        self.assert_union(graph, [])

    def test_package_final_uses_producers_already_registered_by_its_union_context(self):
        graph = self.make(fixture("COPY_FILE(${BINDIR}/future.dat consumed.dat)\nFILES(start.dat)\n"
            "FROM_SANDBOX(246 OUT future.dat secondary.dat)\nCOPY_FILE(end.dat installed.dat)\n",
            opener="PACKAGE(satchel)"))
        names = ["start.dat", "future.dat", "secondary.dat", "installed.dat", "consumed.dat"]
        self.assert_union(graph, names)
        package = lib.node_by_output(graph, PREFIX + "satchel.final.pkg.fake")
        self.assertEqual(package["outputs"], [PREFIX + "satchel.final.pkg.fake",
            *[PREFIX + n for n in ("consumed.dat", "start.dat", "future.dat", "secondary.dat", "installed.dat")],
            *[PREFIX + "satchel/" + n for n in names]])
        copies = [n for n in graph["graph"] if n["kv"].get("p") == "CP" and PREFIX + "consumed.dat" in n["outputs"]]
        self.assertEqual(len(copies), 1)
        self.assertIn(copies[0]["uid"], package["deps"])

    def test_source_shadow_is_ready_without_waiting_for_same_named_build_output(self):
        files = fixture("COPY_FILE(future.dat consumed.dat)\nFILES(start.dat)\n"
            "FROM_SANDBOX(246 OUT future.dat secondary.dat)\nCOPY_FILE(end.dat installed.dat)\n")
        files["satchel/future.dat"] = "real source shadow\n"
        graph = self.make(files)
        self.assert_union(graph, ["consumed.dat", "start.dat", "future.dat", "secondary.dat", "installed.dat"])
        self.assert_copy(graph, "$(S)/satchel/future.dat", "consumed.dat")

    def test_genuine_build_input_cycle_keeps_the_defensive_diagnostic(self):
        result = lib.make_process(fixture("COPY_FILE(${BINDIR}/second.dat first.dat)\n"
            "COPY_FILE(${BINDIR}/first.dat second.dat)\n"), "satchel", "--sandboxing")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("dependency cycle among generating macros", result.stderr)

    def test_bundle_keeps_its_declaration_place_and_reuses_the_real_module_producer(self):
        graph = self.make(fixture("FILES(start.dat)\nBUNDLE(tools/stamper NAME tool.copy)\n"
            "FROM_SANDBOX(246 OUT middle.dat)\nCOPY_FILE(end.dat installed.dat)\n",
            opener="PACKAGE(satchel)"))
        self.assert_union(graph, ["start.dat", "tool.copy", "middle.dat", "installed.dat"])
        bundle = lib.node_by_output(graph, PREFIX + "tool.copy")
        tool = lib.node_by_output(graph, "$(B)/tools/stamper/stamper")
        self.assertIn(tool["outputs"][0], bundle["inputs"])
        self.assertIn(tool["uid"], bundle["deps"])
        self.assertEqual(bundle["cmds"][0]["cmd_args"][-2:], [tool["outputs"][0], PREFIX + "tool.copy"])
        package = lib.node_by_output(graph, PREFIX + "satchel.final.pkg.fake")
        self.assertIn(bundle["uid"], package["deps"])
        self.assertEqual(len([n for n in graph["graph"] if n["kv"].get("p") == "BN"]), 1)

    def test_plugin_generated_copy_and_resource_calls_keep_argument_order(self):
        files = fixture("LARGE_FILES(remote.dat start.dat another.dat end.dat)\n")
        files["satchel/remote.dat.external"] = json.dumps({"storage": "SANDBOX", "resource_id": 246})
        files["satchel/another.dat.external"] = json.dumps({"storage": "SANDBOX", "resource_id": 468})
        graph = self.make(files)
        self.assert_union(graph, ["remote.dat", "start.dat", "another.dat", "end.dat"])
        for name in ("start.dat", "end.dat"):
            self.assert_copy(graph, "$(S)/satchel/" + name, name)

    def test_package_consumers_retain_peer_late_output_order_in_either_root_order(self):
        files = fixture("COPY_FILE(${BINDIR}/future.dat consumed.dat)\nFILES(start.dat)\n"
            "FROM_SANDBOX(246 OUT future.dat secondary.dat)\nCOPY_FILE(end.dat installed.dat)\n")
        files.update({"crate/ya.make": "PACKAGE(crate)\nFILES(note.dat)\nFROM_SANDBOX(468 OUT local.dat)\nPEERDIR(satchel)\nEND()\n",
                      "crate/note.dat": "own\n", "outer/ya.make": "PACKAGE(outer)\nPEERDIR(crate)\nEND()\n"})
        for order in (("crate", "satchel"), ("satchel", "crate")):
            with self.subTest(order=order):
                graph = self.make(files, *order, "outer")
                names = ["start.dat", "future.dat", "secondary.dat", "installed.dat", "consumed.dat"]
                self.assert_union(graph, names)
                own = self.assert_union(graph, ["note.dat", "local.dat"], "crate")
                late = [PREFIX + n for n in names] + ["$(B)/crate/note.dat", "$(B)/crate/local.dat"]
                for module in ("crate", "outer"):
                    pk = lib.node_by_output(graph, "$(B)/" + module + "/" + module + ".final.pkg.fake")
                    local = ["$(B)/crate/note.dat", "$(B)/crate/local.dat"] if module == "crate" else []
                    self.assertEqual(pk["outputs"], ["$(B)/" + module + "/" + module + ".final.pkg.fake", *local,
                        *["$(B)/" + module + "/" + p.removeprefix("$(B)/") for p in late]])
                    args = pk["cmds"][1]["cmd_args"]
                    self.assertEqual(args[args.index("--build-root") + 2:-1], late)
                    self.assertIn("$(B)/satchel/satchel.pkg.fake", pk["inputs"])
                    self.assertIn("$(B)/crate/crate.pkg.fake", pk["inputs"])
                    self.assertIn(own["uid"], pk["deps"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
