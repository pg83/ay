"""Native Possible/Required tool edges and the later foreign-platform merge."""
import json
import unittest

import lib
import test_exec_make as execution
import test_yql


BARE = "NO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
FLAGS = (
    "--target-platform-build-type", "relwithdebinfo",
    "--host-build-type", "release",
    "--host-platform-flag", "IS_CROSS_TOOLS=yes",
)


def fixture(used=False, listed=True, ordinary=True):
    files = {
        "grove/emitter/ya.make": (
            "PROGRAM(emitter)\n" + BARE + "ALLOCATOR(FAKE)\n"
            "IF (IS_CROSS_TOOLS)\nCFLAGS(-DTOOL_CONTEXT)\nELSE()\nCFLAGS(-DTARGET_CONTEXT)\nENDIF()\n"
            "PEERDIR(grove/leaf)\nSRCS(main.cpp)\nEND()\n"
        ),
        "grove/emitter/main.cpp": "int main(){return 0;}\n",
        "grove/leaf/ya.make": "LIBRARY(leaf)\n" + BARE + "SRCS(leaf.cpp)\nEND()\n",
        "grove/leaf/leaf.cpp": "int leaf;\n",
        "grove/generated/ya.make": (
            "LIBRARY(generated)\n" + BARE +
            "RUN_PROGRAM(grove/emitter -o codegen.h template.h.in IN template.h.in OUT codegen.h)\n" +
            ("SRCS(standalone.cpp)\n" if ordinary else "") + "END()\n"
        ),
        "grove/generated/standalone.cpp": "int standalone;\n",
        "grove/generated/template.h.in": "#pragma once\n",
        "grove/holder/ya.make": "LIBRARY(holder)\n" + BARE + "PEERDIR(grove/generated)\nSRCS(ordinary.cpp public.h)\nEND()\n",
        "grove/holder/ordinary.cpp": "int ordinary;\n",
        "grove/holder/public.h": "#include <grove/generated/codegen.h>\n",
        "grove/app/ya.make": "PROGRAM(app)\n" + BARE + "ALLOCATOR(FAKE)\nSRCS(main.cpp)\nPEERDIR(" + ("grove/holder" if listed else "grove/generated") + ")\nEND()\n",
        "grove/app/main.cpp": (
            ("#include <grove/holder/public.h>\n" if listed else "#include <grove/generated/codegen.h>\n") if used else ""
        ) + "int main(){return 0;}\n",
    }
    return files


def producers(graph, output):
    return [node for node in graph["graph"] if output in node["outputs"]]


class ForeignToolBindingTest(unittest.TestCase):
    def graph(self, files, *targets, strip="0", add=True):
        graph = lib.make(
            files, targets[0] if targets else "grove/app", *targets[1:], *FLAGS,
            *(["--add-modules-to-results"] if add else []),
            env={"YA_STRIP_PACKAGES_FROM_RESULTS": strip},
        )
        ids = {node["uid"] for node in graph["graph"]}
        self.assertTrue(set(graph["result"]) <= ids)
        self.assertTrue(all(set(node["deps"]) <= ids for node in graph["graph"]))
        return graph

    def check_tool(self, graph, host):
        suffix = ".pic.o" if host else ".o"
        compiled = lib.node_by_output(graph, "$(B)/grove/emitter/main.cpp" + suffix)
        command = compiled["cmds"][0]["cmd_args"]
        self.assertIn("-DTOOL_CONTEXT" if host else "-DTARGET_CONTEXT", command)
        self.assertNotIn("-DTARGET_CONTEXT" if host else "-DTOOL_CONTEXT", command)
        tool = lib.node_by_output(graph, "$(B)/grove/emitter/emitter")
        self.assertIn(compiled["uid"], tool["deps"])
        generated = lib.node_by_output(graph, "$(B)/grove/generated/codegen.h")
        self.assertEqual(generated["foreign_deps"]["tool"], [tool["uid"]])
        self.assertIn(tool["uid"], generated["deps"])
        self.assertIn("$(S)/grove/generated/template.h.in", generated["inputs"])
        self.assertIn("$(B)/grove/emitter/emitter", generated["inputs"])
        return compiled, tool, generated

    def test_listed_header_keeps_target_fallback_with_real_generator_inputs(self):
        for strip in ("0", "1"):
            for add in (False, True):
                with self.subTest(strip=strip, add=add):
                    graph = self.graph(fixture(), strip=strip, add=add)
                    _, tool, generated = self.check_tool(graph, False)
                    self.assertFalse(producers(graph, "$(B)/grove/emitter/main.cpp.pic.o"))
                    app = lib.node_by_output(graph, "$(B)/grove/app/app")
                    self.assertIn(generated["uid"], app["deps"])
                    self.assertIn(generated["outputs"][0], app["inputs"])
                    self.assertEqual(tool["uid"] in graph["result"], add)

    def test_compiled_include_requires_host_in_both_header_forms(self):
        for listed in (False, True):
            for strip in ("0", "1"):
                for add in (False, True):
                    with self.subTest(listed=listed, strip=strip, add=add):
                        graph = self.graph(fixture(used=True, listed=listed), strip=strip, add=add)
                        _, _, generated = self.check_tool(graph, True)
                        self.assertFalse(producers(graph, "$(B)/grove/emitter/main.cpp.o"))
                        compiled = lib.node_by_output(graph, "$(B)/grove/app/main.cpp.o")
                        self.assertIn(generated["uid"], compiled["deps"])
                        self.assertIn(generated["outputs"][0], compiled["inputs"])

    def test_unused_possible_tool_is_removed_before_module_promotion(self):
        for strip in ("0", "1"):
            for add in (False, True):
                with self.subTest(strip=strip, add=add):
                    graph = self.graph(fixture(listed=False), strip=strip, add=add)
                    self.assertFalse(any("/grove/emitter/" in output for node in graph["graph"] for output in node["outputs"]))
                    self.assertFalse(producers(graph, "$(B)/grove/generated/codegen.h"))
                    lib.node_by_output(graph, "$(B)/grove/generated/standalone.cpp.o")

    def test_mixed_roots_select_host_independently_of_root_order(self):
        files = fixture()
        files["grove/side/ya.make"] = files["grove/app/ya.make"].replace("PROGRAM(app)", "PROGRAM(side)")
        files["grove/side/main.cpp"] = "#include <grove/holder/public.h>\nint main(){return 0;}\n"
        for targets in (("grove/app", "grove/side"), ("grove/side", "grove/app")):
            with self.subTest(targets=targets):
                graph = self.graph(files, *targets)
                _, tool, generated = self.check_tool(graph, True)
                self.assertEqual(len(producers(graph, tool["outputs"][0])), 1)
                self.assertFalse(producers(graph, "$(B)/grove/emitter/main.cpp.o"))
                for name in ("app", "side"):
                    app = lib.node_by_output(graph, f"$(B)/grove/{name}/{name}")
                    self.assertIn(app["uid"], graph["result"])
                    self.assertIn(generated["uid"], app["deps"])

    def test_explicit_target_and_required_host_both_survive_in_both_orders(self):
        for targets in (("grove/app", "grove/emitter"), ("grove/emitter", "grove/app")):
            with self.subTest(targets=targets):
                graph = self.graph(fixture(used=True), *targets)
                target = lib.node_by_output(graph, "$(B)/grove/emitter/main.cpp.o")
                host = lib.node_by_output(graph, "$(B)/grove/emitter/main.cpp.pic.o")
                tools = producers(graph, "$(B)/grove/emitter/emitter")
                self.assertEqual(len(tools), 2)
                self.assertTrue(all(node["uid"] in graph["result"] for node in tools))
                self.assertEqual({dep for node in tools for dep in node["deps"]} & {target["uid"], host["uid"]}, {target["uid"], host["uid"]})
                generated = lib.node_by_output(graph, "$(B)/grove/generated/codegen.h")
                selected = next(node for node in tools if host["uid"] in node["deps"])
                self.assertEqual(generated["foreign_deps"]["tool"], [selected["uid"]])

    def test_shared_candidate_peer_needed_by_plain_root_survives_both_orders(self):
        files = fixture(used=True)
        files["grove/plain/ya.make"] = "PROGRAM(plain)\n" + BARE + "ALLOCATOR(FAKE)\nPEERDIR(grove/leaf)\nSRCS(main.cpp)\nEND()\n"
        files["grove/plain/main.cpp"] = "int main(){return 0;}\n"
        for targets in (("grove/app", "grove/plain"), ("grove/plain", "grove/app")):
            with self.subTest(targets=targets):
                graph = self.graph(files, *targets)
                self.check_tool(graph, True)
                self.assertFalse(producers(graph, "$(B)/grove/emitter/main.cpp.o"))
                for suffix in (".o", ".pic.o"):
                    lib.node_by_output(graph, "$(B)/grove/leaf/leaf.cpp" + suffix)
                plain = lib.node_by_output(graph, "$(B)/grove/plain/plain")
                self.assertIn(plain["uid"], graph["result"])
                target_peer = next(node for node in producers(graph, "$(B)/grove/leaf/libleaf.a") if lib.node_by_output(graph, "$(B)/grove/leaf/leaf.cpp.o")["uid"] in node["deps"])
                self.assertIn(target_peer["uid"], plain["deps"])

    def test_host_output_name_uses_one_native_move_for_two_consumers(self):
        files = fixture(used=True, listed=False)
        files["grove/emitter/ya.make"] = "IF (IS_CROSS_TOOLS)\nPROGRAM(host_emitter)\nELSE()\nPROGRAM(target_emitter)\nENDIF()\n" + BARE + "ALLOCATOR(FAKE)\nSRCS(main.cpp)\nEND()\n"
        files["grove/generated/ya.make"] = files["grove/generated/ya.make"].replace("END()", "RUN_PROGRAM(grove/emitter OUT second.h)\nEND()")
        files["grove/app/main.cpp"] = "#include <grove/generated/codegen.h>\n#include <grove/generated/second.h>\nint main(){return 0;}\n"
        graph = self.graph(files)
        host = lib.node_by_output(graph, "$(B)/grove/emitter/host_emitter")
        moves = producers(graph, "$(B)/grove/emitter/target_emitter")
        self.assertEqual(len(moves), 1)
        move = moves[0]
        self.assertEqual(move["cmds"], [{"cmd_args": ["/bin/mv", host["outputs"][0], move["outputs"][0]], "cwd": "$(B)"}])
        self.assertEqual(move["inputs"], host["outputs"])
        self.assertEqual(move["deps"], [host["uid"]])
        for header in ("codegen.h", "second.h"):
            generated = lib.node_by_output(graph, "$(B)/grove/generated/" + header)
            self.assertEqual(generated["foreign_deps"]["tool"], [move["uid"]])
            self.assertEqual(generated["cmds"][0]["cmd_args"][0], move["outputs"][0])

    def test_nested_actions_use_the_selected_context_without_replaying_target(self):
        for used in (False, True):
            with self.subTest(used=used):
                files = fixture(used=used)
                files["grove/emitter/ya.make"] = files["grove/emitter/ya.make"].replace("SRCS(main.cpp)", "SRCS(main.cpp)\nRUN_PROGRAM(grove/helper -o generated.cpp OUT generated.cpp)")
                lib.tool_program(files, "grove/helper", "helper")
                graph = self.graph(files)
                self.check_tool(graph, used)
                suffix = ".pic.o" if used else ".o"
                generated = lib.node_by_output(graph, "$(B)/grove/emitter/generated.cpp" + suffix)
                producer = lib.node_by_output(graph, "$(B)/grove/emitter/generated.cpp")
                self.assertIn(producer["uid"], generated["deps"])
                lib.node_by_output(graph, "$(B)/grove/helper/main.cpp" + suffix)

    def test_auxiliary_tool_binding_preserves_source_tokens_and_input_multiplicity(self):
        files = fixture(used=True)
        lib.tool_program(files, "grove/helper", "helper")
        files["grove/generated/ya.make"] = files["grove/generated/ya.make"].replace("OUT codegen.h", "TOOL grove/helper ${ARCADIA_BUILD_ROOT}/grove/helper grove/helper OUT codegen.h")
        graph = self.graph(files)
        tool = lib.node_by_output(graph, "$(B)/grove/emitter/emitter")
        generated = lib.node_by_output(graph, "$(B)/grove/generated/codegen.h")
        helper = lib.node_by_output(graph, "$(B)/grove/helper/helper")
        self.assertEqual(set(generated["foreign_deps"]["tool"]), {helper["uid"], tool["uid"]})
        self.assertIn(helper["uid"], generated["deps"])
        self.assertIn(helper["outputs"][0], generated["inputs"])

    def test_existing_python_action_host_edge_also_supplies_listed_tool(self):
        files = fixture()
        files["grove/side/ya.make"] = "PROGRAM(side)\n" + BARE + "ALLOCATOR(FAKE)\nSRCS(main.cpp)\nRUN_PYTHON3(grove/script.py TOOL grove/emitter OUT generated.cpp)\nEND()\n"
        files["grove/side/main.cpp"] = "int main(){return 0;}\n"
        files["grove/script.py"] = "print(1)\n"
        for targets in (("grove/app", "grove/side"), ("grove/side", "grove/app")):
            with self.subTest(targets=targets):
                graph = self.graph(files, *targets)
                self.check_tool(graph, True)
                self.assertFalse(producers(graph, "$(B)/grove/emitter/main.cpp.o"))

    def test_target_induced_metadata_survives_host_tool_rebinding(self):
        files = fixture(used=True)
        files["grove/emitter/ya.make"] = files["grove/emitter/ya.make"].replace(
            "SRCS(main.cpp)",
            "IF (IS_CROSS_TOOLS)\nINDUCED_DEPS(h+cpp ${ARCADIA_ROOT}/grove/runtime/host.h)\n"
            "ELSE()\nINDUCED_DEPS(h+cpp ${ARCADIA_ROOT}/grove/runtime/target.h)\nENDIF()\nSRCS(main.cpp)",
        )
        files["grove/runtime/host.h"] = "#pragma once\n"
        files["grove/runtime/target.h"] = "#pragma once\n"
        graph = self.graph(files)
        self.check_tool(graph, True)
        compiled = lib.node_by_output(graph, "$(B)/grove/app/main.cpp.o")
        self.assertIn("$(S)/grove/runtime/target.h", compiled["inputs"])
        self.assertNotIn("$(S)/grove/runtime/host.h", compiled["inputs"])

    def test_new_host_includes_are_collected_after_an_earlier_host_tool(self):
        files = fixture(used=True)
        lib.tool_program(files, "grove/old", "old")
        for module in ("grove/old", "grove/emitter"):
            files[module + "/main.cpp"] = '#include "outer.h"\nint main(){return 0;}\n'
            files[module + "/outer.h"] = '#include "inner.h"\n'
            files[module + "/inner.h"] = "#pragma once\n"
        files["grove/side/ya.make"] = "PROGRAM(side)\n" + BARE + "ALLOCATOR(FAKE)\nSRCS(main.cpp)\nRUN_PYTHON3(grove/script.py TOOL grove/old OUT generated.cpp)\nEND()\n"
        files["grove/side/main.cpp"] = "int main(){return 0;}\n"
        files["grove/script.py"] = "print(1)\n"
        graph = self.graph(files, "grove/side", "grove/app")
        self.check_tool(graph, True)
        for module in ("grove/old", "grove/emitter"):
            compiled = lib.node_by_output(graph, "$(B)/" + module + "/main.cpp.pic.o")
            for header in ("outer.h", "inner.h"):
                self.assertIn("$(S)/" + module + "/" + header, compiled["inputs"])

    def test_java_recursive_start_targets_keep_cpp_and_global_build_roots(self):
        files = lib.java_tree(**fixture(used=True))
        files["entry/ya.make"] = "JAVA_LIBRARY()\nRECURSE(../grove/app ../grove/globals)\nEND()\n"
        files["grove/globals/ya.make"] = "LIBRARY(globals)\n" + BARE + "PEERDIR(grove/holder)\nSRCS(GLOBAL marker.cpp)\nEND()\n"
        files["grove/globals/marker.cpp"] = "#include <grove/holder/public.h>\nint marker;\n"
        graph = self.graph(files, "entry")
        self.check_tool(graph, True)
        for output in ("$(B)/grove/app/app", "$(B)/grove/globals/libglobals.global.a"):
            self.assertIn(lib.node_by_output(graph, output)["uid"], graph["result"])
        generated = lib.node_by_output(graph, "$(B)/grove/generated/codegen.h")
        self.assertIn(generated["uid"], lib.node_by_output(graph, "$(B)/grove/globals/marker.cpp.o")["deps"])

    def test_multimodule_global_results_keep_their_generator(self):
        files = test_yql.udf_fixture()
        files.update(fixture(used=True))
        files["grove/variants/ya.make"] = "YQL_UDF(variants)\n" + BARE + "UDF_NO_PROBE()\nPEERDIR(grove/holder)\nSRCS(GLOBAL marker.cpp)\nEND()\n"
        files["grove/variants/marker.cpp"] = "#include <grove/holder/public.h>\nint marker;\n"
        graph = self.graph(files, "grove/variants", "grove/app")
        self.check_tool(graph, True)
        globals_ = [node for node in graph["graph"] if node["outputs"][0].startswith("$(B)/grove/variants/") and node["outputs"][0].endswith(".global.a")]
        self.assertTrue(globals_)
        self.assertTrue(all(node["uid"] in graph["result"] for node in globals_))

    def test_streaming_executor_never_runs_an_unselected_candidate(self):
        for used, listed in ((False, False), (False, True), (True, True)):
            for strip in ("0", "1"):
                for add in (False, True):
                    with self.subTest(used=used, listed=listed, strip=strip, add=add):
                        ws = execution.Workspace(self, fixture(used=used, listed=listed))
                        flags = (*FLAGS, "-j1", *(["--add-modules-to-results"] if add else []))
                        result = ws.make(*flags, targets=("grove/app",), env=ws.env(YA_STRIP_PACKAGES_FROM_RESULTS=strip))
                        built = execution.built_kinds(result.stderr)
                        emitter_objects = [output for kind, output in built if kind == "CC" and "/grove/emitter/" in output]
                        self.assertEqual(emitter_objects, ["$(B)/grove/emitter/main.cpp" + (".pic.o" if used else ".o")] if listed else [])
                        if listed:
                            self.assertTrue(any("$(B)/grove/generated/codegen.h" in json.loads(path.read_text()) for path in ws.uid_files()))


if __name__ == "__main__":
    unittest.main(verbosity=2)
