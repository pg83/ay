"""Module IsInput precedes NoAutoSrc for generated object/archive outputs."""
import unittest

from tst import lib
from tst.make.test_foreign_tool_binding import FLAGS
from tst.make.test_go_tests import go_world


TOOL = "pine/emitter"
BINARY = "$(B)/pine/emitter/emitter"


def files(module="LIBRARY", declaration="OUT part.o", data=False):
    sources = {
        "pine/consumer/ya.make": f"{module}(consumer)\n{lib.BARE_MODULE}"
                                  + ("ALLOCATOR(FAKE)\n" if module == "PROGRAM" else "") +
                                  f"RUN_PROGRAM({TOOL} {declaration})\nEND()\n",
    }
    lib.tool_program(sources, TOOL, "emitter")
    sources[TOOL + "/ya.make"] = sources[TOOL + "/ya.make"].replace(
        "END()", "ALLOCATOR(FAKE)\nIF (IS_CROSS_TOOLS)\nCFLAGS(-DTOOL_CONTEXT)\nELSE()\n"
                 "CFLAGS(-DTARGET_CONTEXT)\nENDIF()\nEND()")
    if data:
        sources[TOOL + "/ya.make"] = sources[TOOL + "/ya.make"].replace(
            "SRCS(main.cpp)", "SRCS(main.cpp GLOBAL stamp.cpp)")
        sources[TOOL + "/stamp.cpp"] = "int stamp;\n"
        sources["pine/consumer/ya.make"] = sources["pine/consumer/ya.make"].replace(
            f"RUN_PROGRAM({TOOL} ", f"RUN_PROGRAM({TOOL} IN ${{ARCADIA_BUILD_ROOT}}/{TOOL}/emitter ")
    return sources


class RunProgramObjectInputsTest(unittest.TestCase):
    def check_action(self, graph, output, module="LIBRARY"):
        generated = lib.node_by_output(graph, "$(B)/pine/consumer/" + output)
        consumer = lib.node_by_output(graph, "$(B)/pine/consumer/" +
                                      ("libconsumer.a" if module == "LIBRARY" else
                                       "consumer.a" if module == "GO_LIBRARY" else "consumer"))
        self.assertIn(generated["uid"], consumer["deps"])
        self.assertEqual(consumer["deps"].count(generated["uid"]), 1)
        self.assertIn("$(B)/pine/consumer/" + output, consumer["inputs"])
        tools = [node for node in graph["graph"] if BINARY in node["outputs"]
                 and node["uid"] in generated["foreign_deps"]["tool"]]
        self.assertEqual(len(tools), 1)
        self.assertIn(tools[0]["uid"], generated["deps"])
        compiled = next(node for node in graph["graph"]
                        if "$(B)/pine/emitter/main.cpp.pic.o" in node["outputs"]
                        and node["uid"] in tools[0]["deps"])
        self.assertIn("-DTOOL_CONTEXT", compiled["cmds"][0]["cmd_args"])
        self.assertNotIn("-DTARGET_CONTEXT", compiled["cmds"][0]["cmd_args"])
        ids = {node["uid"] for node in graph["graph"]}
        self.assertTrue(graph["result"])
        self.assertLessEqual(set(graph["result"]), ids)
        self.assertTrue(all(set(node["deps"]) <= ids for node in graph["graph"]))
        return generated, consumer

    def test_object_outputs_are_direct_module_inputs_even_with_noauto(self):
        for module in ("LIBRARY", "PROGRAM"):
            for keyword, output in (("OUT", "part.o"), ("OUT_NOAUTO", "part.o"),
                                    ("STDOUT", "part.obj"), ("STDOUT_NOAUTO", "part.obj"),
                                    ("OUT_NOAUTO", "packed.a"), ("OUT", "packed.lib")):
                with self.subTest(module=module, keyword=keyword, output=output):
                    graph = lib.make(files(module, f"{keyword} {output}"), "pine/consumer", *FLAGS)
                    self.check_action(graph, output, module)

    def test_secondary_object_keeps_the_real_main_output_and_one_producer(self):
        graph = lib.make(files(declaration="OUT receipt.bin OUT_NOAUTO part.o side.o"),
                         "pine/consumer", *FLAGS)
        action, consumer = self.check_action(graph, "part.o")
        self.assertEqual(action["outputs"], ["$(B)/pine/consumer/receipt.bin",
                                             "$(B)/pine/consumer/part.o", "$(B)/pine/consumer/side.o"])
        self.assertEqual(sum("$(B)/pine/consumer/part.o" in n["outputs"] for n in graph["graph"]), 1)
        self.assertIn("$(B)/pine/consumer/receipt.bin", consumer["inputs"])
        self.assertIn("$(B)/pine/consumer/side.o", consumer["inputs"])

    def test_tool_and_explicit_data_keep_both_contexts_in_either_root_order(self):
        for targets in ((TOOL, "pine/consumer"), ("pine/consumer", TOOL)):
            with self.subTest(targets=targets):
                graph = lib.make(files(data=True), *targets, *FLAGS, "--add-modules-to-results")
                action, _ = self.check_action(graph, "part.o")
                stamp = next(node for node in graph["graph"]
                             if "$(B)/pine/emitter/stamp.cpp.o" in node["outputs"]
                             and node["uid"] in action["deps"])
                self.assertIn(stamp["outputs"][0], action["inputs"])
                self.assertNotIn("$(B)/pine/emitter/stamp.cpp.pic.o", action["inputs"])
                tools = [node for node in graph["graph"] if BINARY in node["outputs"]]
                self.assertEqual(len(tools), 2)
                self.assertTrue(all(node["uid"] in graph["result"] for node in tools))

    def test_unused_noninputs_stay_unused_and_union_keeps_its_own_collector(self):
        for declaration in ("OUT_NOAUTO unused.bin", "OUT_NOAUTO unused.h", "STDOUT_NOAUTO unused.bin"):
            with self.subTest(declaration=declaration):
                sources = files(declaration=declaration)
                sources["pine/consumer/ya.make"] = sources["pine/consumer/ya.make"].replace(
                    "END()", "SRCS(ordinary.cpp)\nEND()")
                sources["pine/consumer/ordinary.cpp"] = "int ordinary;\n"
                graph = lib.make(sources, "pine/consumer", *FLAGS)
                self.assertFalse(any(BINARY in node["outputs"] or node["kv"]["p"] == "PR"
                                     for node in graph["graph"]))
        graph = lib.make(files("UNION", "OUT_NOAUTO part.o"), "pine/consumer", *FLAGS)
        action = lib.node_by_output(graph, "$(B)/pine/consumer/part.o")
        union = lib.node_by_output(graph, "$(B)/pine/consumer/consumer.pkg.fake")
        self.assertIn(action["uid"], union["deps"])
        self.assertFalse(any(node["kv"]["p"] == "AR" for node in graph["graph"]))

    def test_direct_objects_follow_mixed_macro_declaration_order(self):
        for keyword in ("OUT", "OUT_NOAUTO", "STDOUT", "STDOUT_NOAUTO"):
            for program_first in (True, False):
                with self.subTest(keyword=keyword, program_first=program_first):
                    sources = files(declaration=keyword + " part.o")
                    call = f"RUN_PROGRAM({TOOL} {keyword} part.o)\n"
                    scripted = "RUN_PYTHON3(gen.py OUT scripted.o)\n"
                    sources["pine/consumer/ya.make"] = sources["pine/consumer/ya.make"].replace(
                        call, "SRCS(ordinary.cpp)\n" +
                        (call + scripted if program_first else scripted + call))
                    sources["pine/consumer/gen.py"] = "pass\n"
                    sources["pine/consumer/ordinary.cpp"] = "int ordinary;\n"
                    graph = lib.make(sources, "pine/consumer", *FLAGS)
                    _, consumer = self.check_action(graph, "part.o")
                    command = next(cmd["cmd_args"] for cmd in consumer["cmds"]
                                   if "$(S)/build/scripts/link_lib.py" in cmd["cmd_args"])
                    objects = [arg.rsplit("/", 1)[-1] for arg in command
                               if arg.startswith("$(B)/pine/consumer/") and arg.endswith(".o")]
                    self.assertEqual(objects, (["part.o", "scripted.o"] if program_first
                                               else ["scripted.o", "part.o"]) + ["ordinary.cpp.o"])

    def test_go_module_accepts_object_inputs_but_not_lib(self):
        sources = go_world()
        sources.update(files("GO_LIBRARY", "OUT_NOAUTO part.o packed.lib unused.bin"))
        sources["pine/consumer/ya.make"] = sources["pine/consumer/ya.make"].replace(
            "END()", "SRCS(main.go)\nEND()")
        sources["pine/consumer/main.go"] = "package consumer\n"
        graph = lib.make(sources, "pine/consumer", *FLAGS)
        _, consumer = self.check_action(graph, "part.o", "GO_LIBRARY")
        self.assertNotIn("$(B)/pine/consumer/packed.lib", consumer["inputs"])
        self.assertNotIn("$(B)/pine/consumer/unused.bin", consumer["inputs"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
