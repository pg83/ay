import unittest

import lib
from test_foreign_tool_binding import FLAGS
from test_go_tests import go_module, go_world


TOOL = "grove/emitter"


def files(module="GO_LIBRARY", go_tool=False, resource=False, unused=False):
    sources = go_world()
    tool = "library/go/core/resource/cc" if resource else TOOL
    flags = ("IF (IS_CROSS_TOOLS)\nCFLAGS(-DTOOL_CONTEXT)\nELSE()\n"
             "CFLAGS(-DTARGET_CONTEXT)\nENDIF()\n")
    if go_tool:
        go_module(sources, tool, module="GO_PROGRAM", macros=lib.BARE_MODULE + "ALLOCATOR(FAKE)\n" + flags)
    else:
        lib.tool_program(sources, tool, tool.rsplit("/", 1)[-1])
        sources[tool + "/ya.make"] = sources[tool + "/ya.make"].replace("END()", flags + "END()")
    if resource:
        go_module(sources, "library/go/core/resource")
        declaration = "RESOURCE(payload.txt named_payload)\n"
    else:
        declaration = f"RUN_PROGRAM({tool} OUT {'unused.h' if unused else 'created.go'})\n"
    sources.update({
        "grove/consumer/ya.make": f"{module}(consumer)\n{lib.BARE_MODULE}ALLOCATOR(FAKE)\nSRCS(main.go)\n{declaration}END()\n",
        "grove/consumer/main.go": "package main\nfunc main() {}\n",
        "grove/consumer/payload.txt": "payload\n",
    })
    return sources


class GoGeneratedToolContextTest(unittest.TestCase):
    def check_graph(self, graph, module, go_tool=False, resource=False):
        tool = "library/go/core/resource/cc" if resource else TOOL
        binary = "$(B)/" + tool + "/" + tool.rsplit("/", 1)[-1]
        generated = [node for node in graph["graph"] if node["kv"]["p"] == "PR"
                     and any(out.startswith("$(B)/grove/consumer/") and out.endswith(".go")
                             for out in node["outputs"])]
        self.assertEqual(len(generated), 1)
        generated = generated[0]
        linked = [node for node in graph["graph"] if binary in node["outputs"]]
        selected = [node for node in linked if node["uid"] in generated["deps"]]
        self.assertEqual(len(selected), 1)
        commands = [arg for cmd in selected[0]["cmds"] for arg in cmd["cmd_args"]]
        if go_tool:
            self.assertIn("-DTOOL_CONTEXT", commands)
            self.assertNotIn("-DTARGET_CONTEXT", commands)
            self.assertIn("$(B)/" + tool + "/__vcs_version__.c.pic.o", commands)
        else:
            compiled = lib.node_by_output(graph, "$(B)/" + tool + "/main.cpp.pic.o")
            self.assertIn(compiled["uid"], selected[0]["deps"])
            self.assertIn("-DTOOL_CONTEXT", compiled["cmds"][0]["cmd_args"])
        output = "$(B)/grove/consumer/" + ("consumer.a" if module == "GO_LIBRARY" else "consumer")
        consumer = lib.node_by_output(graph, output)
        self.assertIn(generated["outputs"][0], consumer["inputs"])
        self.assertIn(generated["uid"], consumer["deps"])
        self.assertIn(binary, generated["inputs"])
        self.assertEqual(generated["foreign_deps"]["tool"], [selected[0]["uid"]])
        ids = {node["uid"] for node in graph["graph"]}
        self.assertLessEqual(set(graph["result"]), ids)
        self.assertTrue(all(set(node["deps"]) <= ids for node in graph["graph"]))

    def test_generated_go_sources_require_host_tools(self):
        for module in ("GO_LIBRARY", "GO_PROGRAM"):
            for go_tool in (False, True):
                with self.subTest(module=module, go_tool=go_tool):
                    graph = lib.make(files(module, go_tool), "grove/consumer", *FLAGS, "--sandboxing")
                    self.check_graph(graph, module, go_tool)

    def test_go_resource_generator_uses_the_same_source_edge(self):
        graph = lib.make(files(go_tool=True, resource=True), "grove/consumer", *FLAGS, "--sandboxing")
        self.check_graph(graph, "GO_LIBRARY", True, True)
        generated = next(node for node in graph["graph"] if node["kv"]["p"] == "PR"
                         and any(out.startswith("$(B)/grove/consumer/resource.") for out in node["outputs"]))
        self.assertIn("$(S)/grove/consumer/payload.txt", generated["inputs"])

    def test_early_source_use_keeps_one_action_and_its_main_output(self):
        sources = files()
        sources["grove/consumer/ya.make"] = sources["grove/consumer/ya.make"].replace(
            "OUT created.go", "OUT receipt.bin created.go")
        graph = lib.make(sources, "grove/consumer", *FLAGS, "--sandboxing")
        self.check_graph(graph, "GO_LIBRARY")
        actions = [node for node in graph["graph"] if "$(B)/grove/consumer/created.go" in node["outputs"]]
        self.assertEqual(len(actions), 1)
        self.assertEqual(actions[0]["outputs"], ["$(B)/grove/consumer/receipt.bin", "$(B)/grove/consumer/created.go"])
        consumer = lib.node_by_output(graph, "$(B)/grove/consumer/consumer.a")
        self.assertIn("$(B)/grove/consumer/receipt.bin", consumer["inputs"])
        self.assertEqual(consumer["deps"].count(actions[0]["uid"]), 1)

    def test_unused_header_does_not_require_a_host_tool(self):
        graph = lib.make(files(unused=True), "grove/consumer", *FLAGS, "--sandboxing")
        self.assertFalse(any(output.startswith("$(B)/grove/emitter/") or output.endswith("/unused.h")
                             for node in graph["graph"] for output in node["outputs"]))
        lib.node_by_output(graph, "$(B)/grove/consumer/consumer.a")

    def test_explicit_tool_root_retains_both_contexts_in_either_order(self):
        for targets in ((TOOL, "grove/consumer"), ("grove/consumer", TOOL)):
            with self.subTest(targets=targets):
                graph = lib.make(files(go_tool=True), *targets, *FLAGS, "--sandboxing", "--add-modules-to-results")
                self.check_graph(graph, "GO_LIBRARY", True)
                tools = [node for node in graph["graph"] if "$(B)/grove/emitter/emitter" in node["outputs"]]
                self.assertEqual(len(tools), 2)
                self.assertTrue(all(node["uid"] in graph["result"] for node in tools))


if __name__ == "__main__":
    unittest.main(verbosity=2)
