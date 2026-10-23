import base64
import unittest

import lib
from test_python_cython import python_base
from test_python2 import python2_base


def fixture(tag="BOTH", local=False, before=False, producer="sandbox"):
    files = python_base()
    files.update(python2_base())
    resource = "RESOURCE(data.dat payload)\n"
    command = {
        "sandbox": "FROM_SANDBOX(FILE 42 OUT_NOAUTO data.dat)\n",
        "program": "RUN_PROGRAM(tools/gen OUT_NOAUTO data.dat)\n",
        "python": "RUN_PYTHON3(gen.py OUT_NOAUTO data.dat)\n",
    }[producer]
    if producer == "program":
        lib.tool_program(files, "tools/gen", "gen")
    if producer == "python":
        files["mod/gen.py"] = ""
    files["mod/ya.make"] = (
        "PY23_LIBRARY()\n" + lib.BARE_MODULE + "NO_PYTHON_INCLUDES()\n"
        + ("" if tag == "BOTH" else f"ONLY_TAGS({tag})\n")
        + "PY_SRCS(a.py)\n" + (command + resource if before else resource + command) + "END()\n"
    )
    files["mod/a.py"] = ""
    if local:
        files["mod/data.dat"] = "local"
    targets = ["mod"]
    if tag in ("PY2", "BOTH"):
        files["caller/ya.make"] = "PROGRAM()\n" + lib.BARE_MODULE + "USE_PYTHON2()\nPEERDIR(mod)\nEND()\n"
        targets.append("caller")
    return files, targets


def resource_order(graph, py3):
    archive = lib.node_by_output(graph, "$(B)/mod/libpy" + ("3" if py3 else "") + "mod.global.a")
    producers = {out: node for node in graph["graph"] for out in node["outputs"]}
    order = []
    for arg in archive["cmds"][0]["cmd_args"]:
        if "/objcopy_" not in arg:
            continue
        args = producers[arg]["cmds"][0]["cmd_args"]
        if "--keys" not in args:
            order.append("payload" if any(a.startswith("payload=") for a in args) else "namespace")
            continue
        key = base64.b64decode(args[args.index("--keys") + 1]).decode()
        order.append(key if key in ("first", "last") else "payload" if key == "payload" or key == "resfs/file/data.dat" else "python")
    return order


class ResourceReadinessTest(unittest.TestCase):
    def test_resources_keep_native_call_phase_and_prior_availability(self):
        from resource_phase_cases import CASES

        for case in CASES:
            with self.subTest(case=case["name"]):
                files, _ = fixture("PY3")
                files.update(case["files"])
                graph = lib.make(files, "mod", "--target-platform", "default-linux-x86_64")
                archive = lib.node_by_output(graph, case["output"])
                members = [arg for arg in archive["cmds"][0]["cmd_args"] if arg.endswith(".o")]
                self.assertEqual(members, case["members"])
                for output in case["members"]:
                    producer = lib.node_by_output(graph, output)
                    self.assertIn(output, archive["inputs"])
                    self.assertIn(producer["uid"], archive["deps"])

    # Upstream ModuleBuilder::RecursiveAddInputs requeues unresolved actions.
    # Availability is shared by distinct PY2/PY3 module contexts, not their AST.
    def test_same_context_resource_output_collision_is_rejected(self):
        files, _ = fixture("PY3", local=True)
        files["mod/ya.make"] = files["mod/ya.make"].replace(
            "RESOURCE(data.dat payload)", "RESOURCE(- shared=value)\nRESOURCE(- shared=value)",
        )
        result = lib.make_process(files, "mod", "--sandboxing")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("duplicate producer", result.stderr)

    def test_promoted_context_uses_one_producer_per_resource_output(self):
        files, _ = fixture()
        files["mod/ya.make"] += "RECURSE_FOR_TESTS(test)\n"
        files["mod/test/ya.make"] = "PY23_TEST()\n" + lib.BARE_MODULE + "NO_PYTHON_INCLUDES()\nTEST_SRCS(test_api.py)\nPEERDIR(mod)\nEND()\n"
        files["mod/test/test_api.py"] = ""
        files["library/python/pytest/ya.make"] = "LIBRARY()\n" + lib.BARE_MODULE + "END()\n"
        graph = lib.make(files, "mod", "-t", "--sandboxing", opensource=False)
        nodes = {node["uid"]: node for node in graph["graph"]}
        binary = lib.node_by_output(graph, "$(B)/mod/test/mod-test")
        archive = next(nodes[uid] for uid in binary["deps"]
                       if "$(B)/mod/libpy3mod.global.a" in nodes[uid]["outputs"])
        outputs = [out for uid in archive["deps"] for out in nodes[uid]["outputs"]]
        self.assertEqual(len(outputs), 3)
        self.assertEqual(len(set(outputs)), 3)
        py2 = lib.node_by_output(graph, "$(B)/mod/test/py2_mod-test")
        for suite in ("pytest", "py3test", "import_test"):
            run = lib.node_by_output(graph, f"$(B)/mod/test/test-results/{suite}/meta.json")
            self.assertTrue({py2["uid"], binary["uid"]}.issubset(run["deps"]))

    def test_first_and_later_contexts_use_actual_output_availability(self):
        files, targets = fixture()
        graph = lib.make(files, *targets, "--sandboxing")
        self.assertEqual(resource_order(graph, False), ["python", "payload"])
        self.assertEqual(resource_order(graph, True), ["payload", "namespace", "python"])

    def test_py3_only_context_requeues_unavailable_resource(self):
        files, targets = fixture("PY3")
        graph = lib.make(files, *targets, "--sandboxing")
        self.assertEqual(resource_order(graph, True), ["namespace", "python", "payload"])

    def test_local_input_and_earlier_producer_are_ready(self):
        for tag in ("PY2", "PY3"):
            for local, before in ((True, False), (False, True)):
                with self.subTest(tag=tag, local=local, before=before):
                    files, targets = fixture(tag, local=local, before=before)
                    graph = lib.make(files, *targets, "--sandboxing")
                    expected = ["payload", "namespace", "python"] if tag == "PY3" else ["payload", "python"]
                    self.assertEqual(resource_order(graph, tag == "PY3"), expected)

    def test_resource_files_and_text_rootrel_wait_for_later_producers(self):
        for resource in ("RESOURCE_FILES(data.dat)", r'''RESOURCE(- payload=\${rootrel;context=TEXT;input=TEXT:"data.dat"})'''):
            with self.subTest(resource=resource):
                files, targets = fixture("PY3")
                files["mod/ya.make"] = files["mod/ya.make"].replace("RESOURCE(data.dat payload)", resource)
                graph = lib.make(files, *targets, "--sandboxing")
                expected = ["namespace", "python", "payload"]
                self.assertEqual(resource_order(graph, True), expected)

    def test_unavailable_batch_requeues_behind_other_ready_batches(self):
        files, targets = fixture("PY3")
        files["mod/one.dat"] = "first"
        files["mod/two.dat"] = "last"
        files["mod/ya.make"] = files["mod/ya.make"].replace(
            "RESOURCE(data.dat payload)",
            "RESOURCE(one.dat first)\nRESOURCE(data.dat payload)\nRESOURCE(two.dat last)",
        )
        graph = lib.make(files, *targets, "--sandboxing")
        self.assertEqual(resource_order(graph, True), ["first", "last", "namespace", "python", "payload"])

    def test_chained_producer_inputs_require_repeated_fifo_visits(self):
        files, targets = fixture("PY3", producer="program")
        files["mod/ya.make"] = files["mod/ya.make"].replace(
            "RESOURCE(data.dat payload)\nRUN_PROGRAM(tools/gen OUT_NOAUTO data.dat)",
            "RESOURCE(data.dat first)\nRESOURCE(other.dat last)\n"
            "RUN_PROGRAM(tools/gen IN other.dat OUT_NOAUTO data.dat)\n"
            "FROM_SANDBOX(FILE 42 OUT_NOAUTO other.dat)",
        )
        graph = lib.make(files, *targets, "--sandboxing")
        self.assertEqual(resource_order(graph, True), ["namespace", "python", "last", "first"])

    def test_producer_can_transform_an_existing_source_with_the_same_name(self):
        files, targets = fixture("PY3", local=True, producer="program")
        files["mod/ya.make"] = files["mod/ya.make"].replace(
            "RUN_PROGRAM(tools/gen OUT_NOAUTO data.dat)",
            "RUN_PROGRAM(tools/gen IN data.dat OUT_NOAUTO data.dat)",
        )
        graph = lib.make(files, *targets, "--sandboxing")
        self.assertEqual(resource_order(graph, True), ["payload", "namespace", "python"])
        payload = next(node for node in graph["graph"] if node["kv"].get("p") == "PY" and "--keys" in node["cmds"][0]["cmd_args"] and base64.b64encode(b"payload").decode() in node["cmds"][0]["cmd_args"])
        self.assertIn("$(S)/mod/data.dat", payload["inputs"])
        self.assertNotIn("$(B)/mod/data.dat", payload["inputs"])

    def test_cyclic_generated_scripts_fail_without_an_unbounded_queue(self):
        files, targets = fixture("PY3")
        files["mod/ya.make"] = files["mod/ya.make"].replace(
            "FROM_SANDBOX(FILE 42 OUT_NOAUTO data.dat)",
            "RUN_PYTHON3(second.py OUT_NOAUTO first.py)\nRUN_PYTHON3(first.py OUT_NOAUTO second.py)",
        )
        result = lib.make_process(files, targets[0], "--sandboxing", timeout=3)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("resource action inputs never become ready", result.stderr)

    def test_prior_language_context_output_remains_ready_without_redeclaration(self):
        files, targets = fixture()
        files["mod/ya.make"] = files["mod/ya.make"].replace(
            "FROM_SANDBOX(FILE 42 OUT_NOAUTO data.dat)",
            "IF(PYTHON2)\nFROM_SANDBOX(FILE 42 OUT_NOAUTO data.dat)\nENDIF()",
        )
        graph = lib.make(files, *targets, "--sandboxing")
        self.assertEqual(resource_order(graph, False), ["python", "payload"])
        self.assertEqual(resource_order(graph, True), ["payload", "namespace", "python"])

    def test_peer_provider_output_is_ready_in_the_consuming_context(self):
        files, targets = fixture("PY3")
        files["provider/ya.make"] = "LIBRARY()\n" + lib.BARE_MODULE + "FROM_SANDBOX(FILE 42 OUT_NOAUTO foreign.dat)\nEND()\n"
        files["mod/ya.make"] = files["mod/ya.make"].replace(
            "RESOURCE(data.dat payload)",
            "PEERDIR(provider)\nRESOURCE(../provider/foreign.dat payload)",
        ).replace("FROM_SANDBOX(FILE 42 OUT_NOAUTO data.dat)\n", "")
        graph = lib.make(files, *targets, "--sandboxing")
        self.assertEqual(resource_order(graph, True), ["payload", "namespace", "python"])

    def test_repeated_source_resource_keys_preserve_the_first_binding(self):
        files, targets = fixture("PY3", local=True)
        files["mod/ya.make"] = files["mod/ya.make"].replace(
            "RESOURCE(data.dat payload)", "RESOURCE(data.dat first data.dat second)",
        )
        graph = lib.make(files, *targets, "--sandboxing")
        self.assertEqual(resource_order(graph, True), ["first", "namespace", "python"])
        payload = next(node for node in graph["graph"] if node["kv"].get("p") == "PY" and base64.b64encode(b"second").decode() in node["cmds"][0]["cmd_args"])
        self.assertIn("$(S)/mod/data.dat", payload["inputs"])
        self.assertNotIn("$(B)/mod/data.dat", payload["inputs"])

    def test_unselected_multimodule_sibling_executes_before_the_start_context(self):
        files, _targets = fixture()
        graph = lib.make(files, "mod", "--sandboxing")
        self.assertEqual(resource_order(graph, True), ["payload", "namespace", "python"])
        outputs = [output for node in graph["graph"] for output in node["outputs"]]
        self.assertFalse(any(output.endswith("/libpymod.global.a") for output in outputs))
        self.assertFalse(any("/tools/py2cc/" in output for output in outputs))

    def test_program_and_python_producers_use_the_same_readiness_rule(self):
        for producer in ("program", "python"):
            for before in (False, True):
                with self.subTest(producer=producer, before=before):
                    files, targets = fixture("PY3", before=before, producer=producer)
                    graph = lib.make(files, *targets, "--sandboxing")
                    expected = ["payload", "namespace", "python"] if before else ["namespace", "python", "payload"]
                    self.assertEqual(resource_order(graph, True), expected)


if __name__ == "__main__":
    unittest.main(verbosity=2)
