import unittest

from tst import lib
from tst.make.test_foreign_tool_binding import FLAGS
from tst.make.test_go_generated_tool_context import TOOL, files


def embed_files(module="GO_LIBRARY"):
    sources = files(module, go_tool=True)
    sources["grove/consumer/ya.make"] = sources["grove/consumer/ya.make"].replace(
        f"RUN_PROGRAM({TOOL} OUT created.go)",
        f"RUN_PROGRAM({TOOL} OUT_NOAUTO catalog.pb)\nGO_EMBED_BINDIR(catalog.pb FILES catalog.pb)")
    return sources


def data_files():
    sources = files()
    sources[TOOL + "/ya.make"] = sources[TOOL + "/ya.make"].replace(
        "SRCS(main.cpp)", "SRCS(main.cpp GLOBAL stamp.cpp)")
    sources[TOOL + "/stamp.cpp"] = "int data_stamp;\n"
    sources["grove/consumer/ya.make"] = sources["grove/consumer/ya.make"].replace(
        f"RUN_PROGRAM({TOOL} OUT created.go)",
        f"RUN_PROGRAM({TOOL} IN ${{ARCADIA_BUILD_ROOT}}/{TOOL}/emitter OUT created.go)")
    return sources


class GoGeneratedEmbedContextTest(unittest.TestCase):
    def test_generated_embed_is_a_real_module_build_input(self):
        for module in ("GO_LIBRARY", "GO_PROGRAM"):
            with self.subTest(module=module):
                graph = lib.make(embed_files(module), "grove/consumer", *FLAGS, "--sandboxing")
                generated = lib.node_by_output(graph, "$(B)/grove/consumer/catalog.pb")
                tool = lib.node_by_output(graph, "$(B)/grove/emitter/emitter")
                commands = [arg for cmd in tool["cmds"] for arg in cmd["cmd_args"]]
                self.assertIn("-DTOOL_CONTEXT", commands)
                self.assertNotIn("-DTARGET_CONTEXT", commands)
                self.assertIn(tool["uid"], generated["deps"])
                consumer = lib.node_by_output(graph, "$(B)/grove/consumer/" +
                                              ("consumer.a" if module == "GO_LIBRARY" else "consumer"))
                self.assertIn(generated["outputs"][0], consumer["inputs"])
                self.assertIn(generated["uid"], consumer["deps"])
                self.assertTrue(any("++embed" in cmd["cmd_args"] for cmd in consumer["cmds"]))

    def test_source_generator_binary_also_used_as_data_keeps_global_inputs(self):
        graph = lib.make(data_files(), TOOL, "grove/consumer", *FLAGS, "--sandboxing")
        generated = lib.node_by_output(graph, "$(B)/grove/consumer/created.go")
        stamp = next(node for node in graph["graph"]
                     if "$(B)/grove/emitter/stamp.cpp.o" in node["outputs"]
                     and node["uid"] in generated["deps"])
        self.assertIn(stamp["outputs"][0], generated["inputs"])
        self.assertIn(stamp["uid"], generated["deps"])
        self.assertNotIn("$(B)/grove/emitter/stamp.cpp.pic.o", generated["inputs"])
        host = lib.node_by_output(graph, "$(B)/grove/emitter/main.cpp.pic.o")
        tools = [node for node in graph["graph"] if "$(B)/grove/emitter/emitter" in node["outputs"]]
        selected = next(node for node in tools if host["uid"] in node["deps"])
        self.assertIn(selected["uid"], generated["deps"])
        self.assertEqual(generated["foreign_deps"]["tool"], [selected["uid"]])
        ids = {node["uid"] for node in graph["graph"]}
        self.assertLessEqual(set(graph["result"]), ids)
        self.assertTrue(all(set(node["deps"]) <= ids for node in graph["graph"]))


if __name__ == "__main__":
    unittest.main(verbosity=2)
