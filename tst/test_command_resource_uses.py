"""External resource dependencies retain the origin of emitted command values."""
import unittest

import lib


def fixture(body, extension="cpp", own=False):
    declaration = "DECLARE_EXTERNAL_RESOURCE(MINT sbr:741 MINTER sbr:742 CEDAR sbr:743 UNUSED sbr:744)\n"
    files = {
        "grove/ya.make": "LIBRARY(grove)\n" + lib.BARE_MODULE
            + (declaration if own else "PEERDIR(resource)\n")
            + body + f"SRCS(main.{extension})\nEND()\n",
        "grove/main." + extension: "int value;\n",
        "resource/ya.make": "RESOURCES_LIBRARY()\n" + declaration + "END()\n",
    }
    return files


def fetches(graph):
    return {node["outputs"][0]: node for node in graph["graph"] if node["kv"]["p"] == "FT"}


class CommandResourceUsesTest(unittest.TestCase):
    def graph(self, files, *roots):
        graph = lib.make(files, *(roots or ("grove",)), "--sandboxing")
        ids = {node["uid"] for node in graph["graph"]}
        self.assertTrue(graph["result"])
        self.assertLessEqual(set(graph["result"]), ids)
        self.assertTrue(all(set(node["deps"]) <= ids for node in graph["graph"]))
        return graph

    def test_binding_origin_distinguishes_identical_printed_arguments(self):
        for variable, resource in (("${MINT_RESOURCE_GLOBAL}ER", "MINT"),
                                   ("${MINTER_RESOURCE_GLOBAL}", "MINTER")):
            with self.subTest(variable=variable):
                graph = self.graph(fixture("CFLAGS(-DNAME=" + variable + ")\n"))
                compiled = lib.node_by_output(graph, "$(B)/grove/main.cpp.o")
                self.assertIn("-DNAME=$(B)/resources/MINTER", compiled["cmds"][0]["cmd_args"])
                selected = lib.node_by_output(graph, "$(B)/resources/" + resource)
                self.assertIn(selected["uid"], compiled["deps"])
                self.assertNotIn("$(B)/resources/" + ({"MINT", "MINTER"} - {resource}).pop(), fetches(graph))
                self.assertNotIn("$(B)/resources/UNUSED", fetches(graph))

    def test_aliases_retain_origin_but_literal_build_paths_do_not(self):
        for own in (False, True):
            with self.subTest(own=own):
                graph = self.graph(fixture(
                    "SET(BARE $MINT_RESOURCE_GLOBAL)\nSET(FROZEN ${MINT_RESOURCE_GLOBAL})\nSET(R2 $BARE)\n"
                    "CFLAGS(-DBARE=$BARE/include -DFROZEN=$FROZEN/include -DRECURSIVE=$R2/include "
                    "-DLITERAL=${ARCADIA_BUILD_ROOT}/resources/UNUSED/include)\n", own=own))
                compiled = lib.node_by_output(graph, "$(B)/grove/main.cpp.o")
                for name in ("BARE", "FROZEN", "RECURSIVE"):
                    self.assertIn(f"-D{name}=$(B)/resources/MINT/include", compiled["cmds"][0]["cmd_args"])
                fetched = lib.node_by_output(graph, "$(B)/resources/MINT")
                self.assertIn(fetched["uid"], compiled["deps"])
                self.assertEqual(compiled["deps"].count(fetched["uid"]), 1)
                self.assertNotIn("$(B)/resources/UNUSED", fetches(graph))
                self.assertNotIn("$(B)/resources/MINTER", fetches(graph))

    def test_unused_language_buckets_and_unconsumed_producers_do_not_hold_resources(self):
        for extension, selected in (("c", "CEDAR"), ("cpp", "MINT")):
            for macro in ("", "GLOBAL "):
                with self.subTest(extension=extension, macro=macro):
                    graph = self.graph(fixture(
                        f"CXXFLAGS({macro}-DCPP=${{MINT_RESOURCE_GLOBAL}})\n"
                        f"CONLYFLAGS({macro}-DC=${{CEDAR_RESOURCE_GLOBAL}})\n", extension=extension))
                    compiled = lib.node_by_output(graph, f"$(B)/grove/main.{extension}.o")
                    fetched = lib.node_by_output(graph, "$(B)/resources/" + selected)
                    self.assertIn(fetched["uid"], compiled["deps"])
                    self.assertNotIn("$(B)/resources/" + ({"MINT", "CEDAR"} - {selected}).pop(), fetches(graph))
        files = fixture("RUN_PROGRAM(tools/write ${CEDAR_RESOURCE_GLOBAL} OUT_NOAUTO unused.bin)\n", own=True)
        lib.tool_program(files, "tools/write", "writer")
        graph = self.graph(files)
        self.assertNotIn("$(B)/grove/unused.bin", {out for n in graph["graph"] for out in n["outputs"]})
        self.assertNotIn("$(B)/resources/CEDAR", fetches(graph))

    def test_python_and_lua_generated_commands_retain_resource_values(self):
        for macro, script, tool in (("RUN_PYTHON3", "gen.py", None), ("RUN_LUA", "gen.lua", "tools/lua")):
            with self.subTest(macro=macro):
                files = fixture(f"{macro}({script} literal value=${{CEDAR_RESOURCE_GLOBAL}} "
                                "ENV DATA=${CEDAR_RESOURCE_GLOBAL}/env OUT generated.h)\n")
                files["grove/" + script] = "# fixture\n"
                files["grove/main.cpp"] = '#include "grove/generated.h"\n'
                if tool:
                    lib.tool_program(files, tool, "lua")
                graph = self.graph(files)
                generated = lib.node_by_output(graph, "$(B)/grove/generated.h")
                fetched = lib.node_by_output(graph, "$(B)/resources/CEDAR")
                self.assertIn(fetched["uid"], generated["deps"])
                self.assertIn("value=$(B)/resources/CEDAR", generated["cmds"][0]["cmd_args"])
                self.assertEqual(generated["cmds"][0]["env"]["DATA"], "$(B)/resources/CEDAR/env")
                self.assertIn("$(S)/grove/" + script, generated["inputs"])
                if tool:
                    lua = lib.node_by_output(graph, "$(B)/tools/lua/lua")
                    self.assertIn(lua["uid"], generated["deps"])
                    self.assertIn(lua["outputs"][0], generated["inputs"])

    def test_sfdl_preprocess_command_keeps_its_consumed_flags(self):
        files = fixture("CFLAGS(-DTABLE=${MINT_RESOURCE_GLOBAL})\nSRCS(table.sfdl)\n")
        files["grove/main.cpp"] = '#include "grove/table"\n'
        files["grove/table.sfdl"] = "fixture\n"
        lib.tool_program(files, "tools/calcstaticopt", "calcstaticopt")
        graph = self.graph(files)
        generated = lib.node_by_output(graph, "$(B)/grove/table")
        fetched = lib.node_by_output(graph, "$(B)/resources/MINT")
        self.assertIn(fetched["uid"], generated["deps"])
        self.assertIn("-DTABLE=$(B)/resources/MINT", generated["cmds"][0]["cmd_args"])
        tool = lib.node_by_output(graph, "$(B)/tools/calcstaticopt/calcstaticopt")
        self.assertIn(tool["uid"], generated["deps"])
        self.assertIn(tool["outputs"][0], generated["inputs"])

    def test_vcs_compile_keeps_peer_global_flags_without_own_exports(self):
        files = fixture("CFLAGS(GLOBAL -DOWN=${MINT_RESOURCE_GLOBAL})\nPEERDIR(palette)\n")
        files["grove/ya.make"] = files["grove/ya.make"].replace("LIBRARY(grove)", "PROGRAM(grove)")
        files["palette/ya.make"] = ("LIBRARY()\n" + lib.BARE_MODULE + "PEERDIR(resource)\n"
                                    "CFLAGS(GLOBAL -DPEER=${CEDAR_RESOURCE_GLOBAL})\nEND()\n")
        graph = self.graph(files)
        linked = lib.node_by_output(graph, "$(B)/grove/grove")
        vcs = next(command["cmd_args"] for command in linked["cmds"]
                   if "$(B)/grove/__vcs_version__.c" in command["cmd_args"] and "-c" in command["cmd_args"])
        self.assertIn("-DPEER=$(B)/resources/CEDAR", vcs)
        self.assertNotIn("-DOWN=$(B)/resources/MINT", vcs)
        fetched = lib.node_by_output(graph, "$(B)/resources/CEDAR")
        self.assertIn(fetched["uid"], linked["deps"])

    def test_generator_argument_and_environment_resources_are_real_dependencies(self):
        files = fixture("RUN_PROGRAM(tools/write resource=${CEDAR_RESOURCE_GLOBAL}/asset "
                        "ENV DATA=${CEDAR_RESOURCE_GLOBAL}/env OUT generated.h)\n")
        files["grove/main.cpp"] = '#include "grove/generated.h"\n'
        lib.tool_program(files, "tools/write", "writer")
        graph = self.graph(files)
        generated = lib.node_by_output(graph, "$(B)/grove/generated.h")
        fetched = lib.node_by_output(graph, "$(B)/resources/CEDAR")
        self.assertIn(fetched["uid"], generated["deps"])
        self.assertEqual(generated["deps"].count(fetched["uid"]), 1)
        self.assertEqual(generated["cmds"][0]["env"]["DATA"], "$(B)/resources/CEDAR/env")
        self.assertIn("resource=$(B)/resources/CEDAR/asset", generated["cmds"][0]["cmd_args"])
        self.assertNotIn(fetched["outputs"][0], generated["inputs"])
        tool = lib.node_by_output(graph, "$(B)/tools/write/writer")
        self.assertIn(tool["uid"], generated["deps"])
        self.assertIn(tool["outputs"][0], generated["inputs"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
