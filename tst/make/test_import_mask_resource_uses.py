"""Final import-mask resource values keep their declared resource dependencies."""
import hashlib
import unittest

from tst import lib
from tst.make.test_python_import_mask_expressions import pathid, program
from tst.make.test_python_srcs import python_base


class ImportMaskResourceUsesTest(unittest.TestCase):
    def graph(self, raw, own):
        files = python_base()
        declaration = "DECLARE_EXTERNAL_RESOURCE(MINT sbr:731 MINTER sbr:732 UNUSED sbr:733)\n"
        program(files, "grove/check", "NO_CHECK_IMPORTS(" + raw + ")\n"
                + (declaration if own else "PEERDIR(grove/assets)\n"))
        files["grove/assets/ya.make"] = "RESOURCES_LIBRARY()\n" + declaration + "END()\n"
        graph = lib.make(files, "grove/check", "--sandboxing")
        ids = {node["uid"] for node in graph["graph"]}
        self.assertTrue(graph["result"])
        self.assertTrue(set(graph["result"]) <= ids)
        self.assertTrue(all(set(node["deps"]) <= ids for node in graph["graph"]))
        key = "py/no_check_imports/" + pathid(raw)
        node = next(node for node in graph["graph"]
                    if any(arg.startswith(key + "=") for cmd in node["cmds"] for arg in cmd["cmd_args"]))
        return graph, node, key

    def test_raw_and_objcopy_keep_consumed_resource_and_real_producer_chain(self):
        for raw in ("$MINT_RESOURCE_GLOBAL/mask", "${ARCADIA_BUILD_ROOT}/literal $MINT_RESOURCE_GLOBAL/mask"):
            for own in (False, True):
                with self.subTest(raw=raw, own=own):
                    graph, node, key = self.graph(raw, own)
                    fetched = lib.node_by_output(graph, "$(B)/resources/MINT")
                    self.assertIn(fetched["uid"], node["deps"])
                    self.assertEqual(node["deps"].count(fetched["uid"]), 1)
                    self.assertNotIn(fetched["outputs"][0], node["inputs"])
                    expected = "$(B)/literal $(B)/resources/MINT/mask" if "ARCADIA_BUILD_ROOT" in raw else "$(B)/resources/MINT/mask"
                    self.assertIn(key + "=" + expected, node["cmds"][0]["cmd_args"])
                    tool = lib.node_by_output(graph, "$(B)/tools/rescompiler/rescompiler")
                    self.assertIn(tool["uid"], node["deps"])
                    self.assertIn(tool["outputs"][0], node["inputs"])
                    linked = lib.node_by_output(graph, "$(B)/grove/check/runner")
                    if "ARCADIA_BUILD_ROOT" in raw:
                        self.assertTrue(node["outputs"][0].endswith("_raw.auxcpp"))
                        compiled = next(n for n in graph["graph"] if node["outputs"][0] in n["inputs"])
                        self.assertIn(node["uid"], compiled["deps"])
                        self.assertIn(compiled["uid"], linked["deps"])
                    else:
                        kv = key + '=\"' + raw + '\"'
                        digest = hashlib.md5((",".join(sorted([kv, "$S/grove/check"])) + "PY3").encode()).hexdigest()[:26]
                        self.assertEqual(node["outputs"], [f"$(B)/grove/check/objcopy_{digest}.o"])
                        self.assertIn(node["uid"], linked["deps"])
                    self.assertFalse(any("$(B)/resources/UNUSED" in n["outputs"] for n in graph["graph"]))

    def test_identical_printed_values_retain_only_their_runtime_binding_origin(self):
        for raw, selected in (("${MINT_RESOURCE_GLOBAL}ER", "MINT"), ("${MINTER_RESOURCE_GLOBAL}", "MINTER")):
            with self.subTest(raw=raw):
                graph, node, key = self.graph(raw, False)
                self.assertIn(key + "=$(B)/resources/MINTER", node["cmds"][0]["cmd_args"])
                fetched = lib.node_by_output(graph, "$(B)/resources/" + selected)
                self.assertIn(fetched["uid"], node["deps"])
                unused = ({"MINT", "MINTER"} - {selected}).pop()
                self.assertFalse(any("$(B)/resources/" + unused in n["outputs"] for n in graph["graph"]))
        graph, node, key = self.graph("${ARCADIA_BUILD_ROOT}/resources/UNUSED/literal", True)
        self.assertIn(key + "=$(B)/resources/UNUSED/literal", node["cmds"][0]["cmd_args"])
        self.assertFalse(any("$(B)/resources/UNUSED" in n["outputs"] for n in graph["graph"]))


class ForwardedCompilerResourceUsesTest(unittest.TestCase):
    def test_bound_global_values_keep_only_the_consumed_language_origins(self):
        files = {
            "grove/ya.make": "LIBRARY()\n" + lib.BARE_MODULE
            + "PEERDIR(palette)\nSRCS(main.cpp plain.c leaf.S)\nEND()\n",
            "grove/main.cpp": "int cpp_value;\n",
            "grove/plain.c": "int c_value;\n",
            "grove/leaf.S": "",
            "palette/ya.make": (
                "RESOURCES_LIBRARY()\n"
                "DECLARE_EXTERNAL_RESOURCE(SAGE sbr:751 BIRCH sbr:752 IDLE sbr:753 FERN sbr:754)\n"
                "SET(FORWARDED GLOBAL -DSAGE=${SAGE_RESOURCE_GLOBAL}/include -DPRIVATE=${IDLE_RESOURCE_GLOBAL})\n"
                "SET(RECURSIVE $FORWARDED)\nCFLAGS(${RECURSIVE})\n"
                "CXXFLAGS(GLOBAL -DBIRCH=${BIRCH_RESOURCE_GLOBAL}/cpp)\n"
                "CONLYFLAGS(GLOBAL -DFERN=${FERN_RESOURCE_GLOBAL}/c)\n"
                "CFLAGS(GLOBAL -DLITERAL=${ARCADIA_BUILD_ROOT}/resources/IDLE)\nEND()\n"
            ),
        }
        graph = lib.make(files, "grove")
        for source, selected, excluded in (("main.cpp", "BIRCH", ("FERN",)),
                                            ("plain.c", "FERN", ("BIRCH",)),
                                            ("leaf.S", None, ("BIRCH", "FERN"))):
            with self.subTest(source=source):
                node = lib.node_by_output(graph, "$(B)/grove/" + source + ".o")
                args = node["cmds"][0]["cmd_args"]
                self.assertIn("-DSAGE=$(B)/resources/SAGE/include", args)
                self.assertIn("-DLITERAL=$(B)/resources/IDLE", args)
                self.assertFalse(any(arg.startswith("-DPRIVATE=") for arg in args))
                for name in ("SAGE",) + ((selected,) if selected else ()):
                    fetched = lib.node_by_output(graph, "$(B)/resources/" + name)
                    self.assertEqual(node["deps"].count(fetched["uid"]), 1)
                    self.assertNotIn(fetched["outputs"][0], node["inputs"])
                for name in excluded:
                    self.assertNotIn(lib.node_by_output(graph, "$(B)/resources/" + name)["uid"], node["deps"])
        self.assertFalse(any("$(B)/resources/IDLE" in node["outputs"] for node in graph["graph"]))


if __name__ == "__main__":
    unittest.main(verbosity=2)
