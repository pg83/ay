import unittest

from tst import lib
from tst.make.test_python2 import python2_base
from tst.make.test_python_srcs import python_base


def files(collection="PACKAGE", module="PY2_PROGRAM", mode="own"):
    sources = python_base()
    sources.update(python2_base())
    body = lib.BARE_MODULE
    if module.startswith("PY"):
        body += "NO_PYTHON_INCLUDES()\nNO_IMPORT_TRACING()\nDISABLE(PYTHON_SQLITE3)\n"
        body += "ALLOCATOR(FAKE)\nNO_CHECK_IMPORTS()\nPY_MAIN(main)\nPY_SRCS(TOP_LEVEL main.py)\n"
    else:
        if module in ("PROGRAM", "DLL"):
            body += "ALLOCATOR(FAKE)\n"
        body += "SRCS(main.cpp)\n"
    if mode in ("own", "transit"):
        body += "RESOURCE(payload.txt meadow_payload)\n"
    if mode == "global":
        body += "SRCS(GLOBAL global.cpp)\n"
    if mode == "peer":
        body += "PEERDIR(meadow/data)\n"
    peer = "meadow/relay" if mode == "transit" else "meadow/tool"
    sources.update({
        "meadow/parcel/ya.make": f"{collection}(parcel)\nPEERDIR({peer})\nEND()\n",
        "meadow/tool/ya.make": f"{module}(utility)\n{body}END()\n",
        "meadow/tool/main.py": "print(1)\n",
        "meadow/tool/main.cpp": "int main() { return 0; }\n",
        "meadow/tool/global.cpp": "int global_value;\n",
        "meadow/tool/payload.txt": "payload\n",
        "meadow/relay/ya.make": "UNION(relay)\nPEERDIR(meadow/tool)\nEND()\n",
        "meadow/data/ya.make": f"LIBRARY(data)\n{lib.BARE_MODULE}RESOURCE(data.txt peer_payload)\nEND()\n",
        "meadow/data/data.txt": "peer payload\n",
    })
    if module == "DLL":
        lib.tool_program(sources, "tools/fix_elf", "fix_elf")
    return sources


class PackageGlobalInputsTest(unittest.TestCase):
    def check_globals(self, graph, collection, outputs):
        suffix = ".ci.pkg.fake" if collection == "CI_GROUP" else ".final.pkg.fake"
        package = lib.node_by_output(graph, "$(B)/meadow/parcel/parcel" + suffix)
        self.assertIn(package["uid"], graph["result"])
        argv = [arg for cmd in package["cmds"] for arg in cmd["cmd_args"]]
        for output in outputs:
            producer = lib.node_by_output(graph, output)
            self.assertIn(output, package["inputs"])
            self.assertIn(producer["uid"], package["deps"])
            self.assertNotIn(output, argv)
            self.assertNotIn("$(B)/meadow/parcel/" + output.removeprefix("$(B)/"), package["outputs"])
        self.assertTrue(outputs)
        ids = {node["uid"] for node in graph["graph"]}
        self.assertLessEqual(set(graph["result"]), ids)
        for node in graph["graph"]:
            self.assertLessEqual(set(node["deps"]), ids)

    def test_program_own_resources_are_physical_inputs_without_being_packaged(self):
        for collection, module in (("PACKAGE", "PY2_PROGRAM"), ("PACKAGE", "PY3_PROGRAM"),
                                   ("PACKAGE", "PROGRAM"), ("PACKAGE", "DLL"), ("CI_GROUP", "PY2_PROGRAM")):
            with self.subTest(collection=collection, module=module):
                graph = lib.make(files(collection, module), "meadow/parcel", "--sandboxing")
                linked = lib.node_by_output(graph, "$(B)/meadow/tool/" +
                                            ("libutility.so" if module == "DLL" else "utility"))
                objects = [output for output in linked["inputs"]
                           if output.startswith("$(B)/meadow/tool/objcopy_") and output.endswith(".o")]
                self.check_globals(graph, collection, objects)

    def test_compiled_global_input_has_the_same_graph_only_obligation(self):
        graph = lib.make(files(module="PROGRAM", mode="global"), "meadow/parcel", "--sandboxing")
        self.check_globals(graph, "PACKAGE", ["$(B)/meadow/tool/global.cpp.o"])

    def test_program_globals_survive_explicit_root_order(self):
        for targets in (("meadow/parcel", "meadow/tool"), ("meadow/tool", "meadow/parcel")):
            with self.subTest(targets=targets):
                graph = lib.make(files(), *targets, "--sandboxing")
                linked = lib.node_by_output(graph, "$(B)/meadow/tool/utility")
                objects = [output for output in linked["inputs"]
                           if output.startswith("$(B)/meadow/tool/objcopy_") and output.endswith(".o")]
                self.check_globals(graph, "PACKAGE", objects)

    def test_library_resource_objects_are_consumed_through_its_global_archive(self):
        graph = lib.make(files(module="LIBRARY"), "meadow/parcel", "--sandboxing")
        package = lib.node_by_output(graph, "$(B)/meadow/parcel/parcel.final.pkg.fake")
        archive = lib.node_by_output(graph, "$(B)/meadow/tool/libutility.global.a")
        self.assertIn(archive["uid"], package["deps"])
        self.assertIn("$(B)/meadow/parcel/meadow/tool/libutility.global.a", package["outputs"])
        for output in archive["inputs"]:
            if output.startswith("$(B)/meadow/tool/objcopy_"):
                self.assertNotIn(output, package["inputs"])

    def test_executable_does_not_forward_its_library_peer_globals(self):
        graph = lib.make(files(module="PROGRAM", mode="peer"), "meadow/parcel", "--sandboxing")
        package = lib.node_by_output(graph, "$(B)/meadow/parcel/parcel.final.pkg.fake")
        objects = [output for node in graph["graph"] for output in node["outputs"]
                   if output.startswith("$(B)/meadow/data/objcopy_")]
        self.assertTrue(objects)
        for output in objects:
            self.assertNotIn(output, package["inputs"])
            self.assertNotIn(lib.node_by_output(graph, output)["uid"], package["deps"])

    def test_tool_and_explicit_build_input_have_distinct_edge_roles(self):
        for explicit in (False, "build", "source"):
            for request_program in (False, True):
                with self.subTest(explicit=explicit, request_program=request_program):
                    sources = files(module="PROGRAM")
                    peer = "PEERDIR(meadow/tool)\n" if explicit else ""
                    arg = ""
                    if explicit:
                        root = "ARCADIA_ROOT" if explicit == "source" else "ARCADIA_BUILD_ROOT"
                        arg = f" IN ${{{root}}}/meadow/tool/utility"
                    if explicit == "source":
                        sources["meadow/tool/utility"] = "physical source sentinel\n"
                    sources["meadow/parcel/ya.make"] = (
                        f"UNION(parcel)\n{peer}RUN_PROGRAM(meadow/tool{arg} OUT made.txt)\nEND()\n")
                    targets = ("meadow/parcel", "meadow/tool") if request_program else ("meadow/parcel",)
                    graph = lib.make(sources, *targets, "--sandboxing")
                    generated = lib.node_by_output(graph, "$(B)/meadow/parcel/made.txt")
                    self.assertIn("$(B)/meadow/tool/utility", generated["inputs"])
                    if explicit == "source":
                        self.assertIn("$(S)/meadow/tool/utility", generated["inputs"])
                    self.assertTrue(any("$(B)/meadow/tool/utility" in node["outputs"]
                                        and node["uid"] in generated["deps"] for node in graph["graph"]))
                    objects = [output for node in graph["graph"] for output in node["outputs"]
                               if output.startswith("$(B)/meadow/tool/objcopy_")]
                    self.assertTrue(objects)
                    for output in set(objects):
                        self.assertEqual(output in generated["inputs"], explicit == "build")
                        self.assertEqual(any(output in node["outputs"] and node["uid"] in generated["deps"]
                                             for node in graph["graph"]), explicit == "build")

    def test_union_does_not_materialize_unused_executable(self):
        graph = lib.make(files(collection="UNION"), "meadow/parcel", "--sandboxing")
        union = lib.node_by_output(graph, "$(B)/meadow/parcel/parcel.pkg.fake")
        self.assertEqual(union["inputs"], [])
        self.assertFalse(any(output.startswith("$(B)/meadow/tool/")
                             for node in graph["graph"] for output in node["outputs"]))


if __name__ == "__main__":
    unittest.main(verbosity=2)
