import json
import unittest

import lib
from test_python2 import CYTHON_HEADERS, CYTHON_UTILITY, CONTRIB_LIBS_PYTHON, python2_base
from test_python_cython import python_base
from test_proto_py_variant import py_proto_files
from test_python_swig import python_base as swig_base


def construction_files(selector="", empty_first=False):
    files = python2_base()
    files.update(python_base())
    files["contrib/libs/python/ya.make"] = CONTRIB_LIBS_PYTHON
    files["contrib/tools/cython_py2/cython.py"] = ""
    for name in CYTHON_HEADERS:
        files[name] = ""
    for name in CYTHON_UTILITY:
        files[f"contrib/tools/cython_py2/Cython/Utility/{name}"] = ""
    cython = "BUILDWITH_CYTHON_CPP(fast.pyx)\n"
    if empty_first:
        cython = "IF (MODULE_TAG != PY2)\n" + cython + "ENDIF()\n"
    files.update({
        "lib/ya.make": "PY23_LIBRARY()\n" + lib.BARE_MODULE + selector
            + "IF (MODULE_TAG == PY2)\nADDINCL(FOR cython custom/py2)\n"
            + "RUN_PROGRAM(tools/unused IN unused.in OUT_NOAUTO unused.h)\n"
            + "RUN_PYTHON3(unused.py TOOL tools/unused_python OUT_NOAUTO unused_python.h)\n"
            + "ELSE()\nADDINCL(FOR cython custom/py3)\nENDIF()\n" + cython + "END()\n",
        "lib/fast.pyx": "from shared.wrapper cimport value\n",
        "lib/unused.in": "",
        "lib/unused.py": "",
        "shared/wrapper.pxd": "from selected.types cimport value\n",
        "custom/py2/selected/types.pxd": "",
        "custom/py3/selected/types.pxd": "",
    })
    lib.tool_program(files, "tools/unused", "unused")
    lib.tool_program(files, "tools/unused_python", "unused_python")
    return files


def cython_nodes(graph):
    return [n for n in graph["graph"] if n.get("kv", {}).get("p") == "CY"]


def promotion_files():
    files = construction_files("INCLUDE_TAGS(PY2)\n")
    files.update({
        "roots/ya.make": "LIBRARY()\n" + lib.BARE_MODULE + "END()\nRECURSE(../lib check)\n",
        "roots/check/ya.make": "EXECTEST()\nRUN(true)\nDEPENDS(tools/selected)\nEND()\n",
    })
    lib.tool_program(files, "tools/selected", "selected")
    files["tools/selected/ya.make"] = files["tools/selected/ya.make"].replace(
        "SRCS(", "USE_PYTHON2()\nPEERDIR(lib)\nSRCS(",
    )
    return files


class ModuleConstructionTest(unittest.TestCase):
    def test_generated_python_metadata_is_retained_until_native_promotion(self):
        files = python_base()
        files.update(swig_base())
        files.update({
            "root/ya.make": "PY3_LIBRARY()\n" + lib.BARE_MODULE + "PEERDIR(lib)\nEND()\nRECURSE(check)\n",
            "root/check/ya.make": "EXECTEST()\nRUN(true)\nDEPENDS(app)\nEND()\n",
            "lib/ya.make": "PY3_LIBRARY()\n" + lib.BARE_MODULE + "PY_SRCS(SWIG_C wrap.swg)\nEND()\n",
            "lib/wrap.swg": '%module wrap\n%header %{\n#include "nested.h"\n%}\n',
            "lib/nested.h": "",
        })
        graph = lib.make(files, "root", "-t")
        generated = lib.node_by_output(graph, "$(B)/lib/wrap.swg.c")
        compiled = lib.node_by_output(graph, "$(B)/lib/wrap.swg.c.o")
        native = lib.node_by_output(graph, "$(B)/lib/libpy3lib.a")
        archive = lib.node_by_output(graph, "$(B)/lib/libpy3lib.global.a")
        program = lib.node_by_output(graph, "$(B)/app/app")
        self.assertIn("$(B)/lib/wrap.py", generated["outputs"])
        self.assertIn("$(S)/lib/nested.h", compiled["inputs"])
        self.assertIn(generated["uid"], compiled["deps"])
        self.assertIn(compiled["uid"], native["deps"])
        self.assertIn(native["uid"], program["deps"])
        self.assertIn(archive["uid"], program["deps"])
        outputs = [out for node in graph["graph"] for out in node["outputs"]]
        self.assertEqual(len(outputs), len(set(outputs)))

    def test_python_proto_metadata_promotes_without_activating_unused_tools(self):
        for linked in (False, True):
            with self.subTest(linked=linked):
                files = py_proto_files()
                files.update({
                    "root/ya.make": "PY3_LIBRARY()\n" + lib.BARE_MODULE + "PEERDIR(p)\nEND()\n",
                    "p/ya.make": "PROTO_LIBRARY()\nEXCLUDE_TAGS(CPP_PROTO)\nSRCS(a.proto)\nEND()\n",
                    "p/a.proto": 'syntax = "proto3";\nmessage A {}\n',
                    "py/ya.make": "PY3_PROGRAM()\nPEERDIR(p)\nPY_SRCS(m.py)\nEND()\n",
                })
                if linked:
                    files["root/ya.make"] += "RECURSE(check)\n"
                    files["root/check/ya.make"] = "EXECTEST()\nRUN(true)\nDEPENDS(py)\nEND()\n"
                graph = lib.make(files, "root", "-t")
                outputs = [out for node in graph["graph"] for out in node["outputs"]]
                self.assertEqual(len(outputs), len(set(outputs)))
                self.assertEqual(linked, "$(B)/p/a__intpy3___pb2.py" in outputs)
                self.assertEqual(linked, "$(B)/contrib/tools/protoc/protoc" in outputs)
                if linked:
                    producer = lib.node_by_output(graph, "$(B)/p/a__intpy3___pb2.py")
                    archive = lib.node_by_output(graph, "$(B)/p/libpy3p.global.a")
                    program = lib.node_by_output(graph, "$(B)/py/py")
                    self.assertIn(archive["uid"], program["deps"])
                    self.assertTrue(any(producer["uid"] in node["deps"] for node in graph["graph"] if "$(B)/p/a__intpy3___pb2.py" in node["inputs"]))

    def test_promotion_preserves_peer_headers_and_cython_metadata(self):
        files = promotion_files()
        files["lib/ya.make"] = files["lib/ya.make"].replace(
            "ADDINCL(FOR cython custom/py2)", "ADDINCL(FOR cython custom/py2)\nPEERDIR(exporter)\nSRCS(public.h)",
        )
        files["lib/public.h"] = '#include "generated.h"\n'
        files["exporter/ya.make"] = (
            "LIBRARY(custom)\n" + lib.BARE_MODULE
            + "ADDINCL(GLOBAL ${BINDIR} GLOBAL FOR cython exported)\n"
            + "RUN_PROGRAM(tools/header OUT_NOAUTO generated.h)\n"
            + "SRCS(GLOBAL ${BINDIR}/generated.h impl.cpp)\nEND()\n"
        )
        files["exporter/impl.cpp"] = "int exporter;\n"
        files["exported/other.pxd"] = ""
        lib.tool_program(files, "tools/header", "header")
        graph = lib.make(files, "roots", "-t")
        archive = lib.node_by_output(graph, "$(B)/exporter/libcustom.a")
        tool = lib.node_by_output(graph, "$(B)/tools/selected/selected")
        producer = lib.node_by_output(graph, "$(B)/exporter/generated.h")
        self.assertIn(archive["uid"], tool["deps"])
        self.assertIn(producer["uid"], tool["deps"])
        self.assertIn("$(B)/exporter/generated.h", tool["inputs"])
        for node in cython_nodes(graph):
            if any(".py2" in out for out in node["outputs"]):
                self.assertIn("-I$(S)/exported", node["cmds"][0]["cmd_args"])

    def test_cython_output_suffixes_keep_public_outputs_in_their_own_variant(self):
        for py2 in (False, True):
            with self.subTest(py2=py2):
                files = construction_files()
                files["lib/ya.make"] = files["lib/ya.make"].replace(
                    "BUILDWITH_CYTHON_CPP(fast.pyx)",
                    "PY_SRCS(CYTHON_CPP fast.pyx CYTHON_C cmod.pyx "
                    "CYTHON_CPP_H hdr_cpp.pyx CYTHON_C_H hdr_c.pyx "
                    "CYTHON_C_API_H api.pyx CYTHONIZE_PY cy.py)",
                )
                for name in ("cmod.pyx", "hdr_cpp.pyx", "hdr_c.pyx", "api.pyx", "cy.py"):
                    files[f"lib/{name}"] = ""
                if py2:
                    files["app/ya.make"] = "PY2_PROGRAM()\n" + lib.BARE_MODULE + "NO_PYTHON_INCLUDES()\nNO_CHECK_IMPORTS()\nPEERDIR(lib)\nPY_SRCS(MAIN main.py)\nEND()\n"
                graph = lib.make(files, "app")
                suffix = ".py2" if py2 else ""
                obj_suffix = ".py2" if py2 else ".py3"
                expected = {
                    (f"$(B)/lib/fast.pyx{obj_suffix}{suffix}.cpp",),
                    (f"$(B)/lib/cmod.pyx{obj_suffix}{suffix}.c",),
                    (f"$(B)/lib/hdr_cpp{suffix}.cpp", f"$(B)/lib/hdr_cpp{suffix}.h"),
                    (f"$(B)/lib/hdr_c{suffix}.c", f"$(B)/lib/hdr_c{suffix}.h"),
                }
                for name in ("api", "cy"):
                    expected.add((f"$(B)/lib/{name}{suffix}.c", f"$(B)/lib/{name}{suffix}.h", f"$(B)/lib/{name}{suffix}_api.h"))
                nodes = cython_nodes(graph)
                self.assertEqual({tuple(node["outputs"]) for node in nodes}, expected)
                distribution = "cython_py2" if py2 else "cython"
                for node in nodes:
                    self.assertIn(f"$(S)/contrib/tools/{distribution}/cython.py", node["inputs"])

    def test_metadata_then_self_then_linked_demand_keeps_each_producer_once(self):
        graph = lib.make(promotion_files(), "roots", "-t")
        self.assertEqual(len(cython_nodes(graph)), 2)
        outputs = [out for node in graph["graph"] for out in node["outputs"]]
        self.assertEqual(len(outputs), len(set(outputs)))
        self.assertFalse(any(out.startswith("$(B)/tools/unused") for out in outputs))
        tool = lib.node_by_output(graph, "$(B)/tools/selected/selected")
        run = lib.node_by_output(graph, "$(B)/roots/check/test-results/exectest/meta.json")
        self.assertIn(tool["uid"], run["deps"])
        for node in cython_nodes(graph):
            self.assertIn("$(S)/custom/py2/selected/types.pxd", node["inputs"])

    def test_promoted_peer_cycle_keeps_each_native_source_once(self):
        files = promotion_files()
        files["lib/ya.make"] = files["lib/ya.make"].replace(
            "ADDINCL(FOR cython custom/py2)", "ADDINCL(FOR cython custom/py2)\nPEERDIR(cycle/a)",
        )
        for name, peer in (("a", "b"), ("b", "a")):
            files[f"cycle/{name}/ya.make"] = "LIBRARY()\n" + lib.BARE_MODULE + f"PEERDIR(cycle/{peer})\nSRCS(x.cpp)\nEND()\n"
            files[f"cycle/{name}/x.cpp"] = f"int {name};\n"
        graph = lib.make(files, "roots", "-t")
        for name in ("a", "b"):
            output = f"$(B)/cycle/{name}/x.cpp.o"
            self.assertEqual(sum(output in n["outputs"] for n in graph["graph"]), 1)
        lib.node_by_output(graph, "$(B)/tools/selected/selected")

    def test_keep_going_restores_the_frame_after_failed_promotion(self):
        files = promotion_files()
        files["lib/ya.make"] = files["lib/ya.make"].replace(
            "ADDINCL(FOR cython custom/py2)", "ADDINCL(FOR cython custom/py2)\nPY_SRCS(bad.py)",
        )
        files["lib/bad.py"] = ""
        for tool in ("tools/py2cc", "tools/py2cc/stage0pycc"):
            files[f"{tool}/ya.make"] = "LIBRARY()\n" + lib.BARE_MODULE + "END()\n"
        files["roots/ya.make"] += "RECURSE(healthy)\n"
        files["roots/healthy/ya.make"] = "LIBRARY()\n" + lib.BARE_MODULE + "SRCS(good.cpp)\nEND()\n"
        files["roots/healthy/good.cpp"] = "int good;\n"
        result = lib.make_process(files, "roots", "-t", "-k")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertNotIn("panic:", result.stderr)
        self.assertIn("has no linkable output", result.stderr)
        self.assertEqual(result.stderr.count("module-failed: lib:"), 1)
        graph = json.loads(result.stdout)
        lib.node_by_output(graph, "$(B)/roots/healthy/good.cpp.o")

    def test_first_variant_owns_shared_resolution_without_exporting_unused_tools(self):
        for selector, empty_first, owner in (
            ("", False, "py2"),
            ("EXCLUDE_TAGS(PY2)\n", False, "py3"),
            ("ONLY_TAGS(PY3)\n", False, "py3"),
            ("", True, "py3"),
        ):
            with self.subTest(selector=selector, empty_first=empty_first):
                graph = lib.make(construction_files(selector, empty_first), "app")
                nodes = cython_nodes(graph)
                self.assertEqual(len(nodes), 1)
                self.assertIn(f"$(S)/custom/{owner}/selected/types.pxd", nodes[0]["inputs"])
                self.assertNotIn(f"$(S)/custom/{'py3' if owner == 'py2' else 'py2'}/selected/types.pxd", nodes[0]["inputs"])
                self.assertIn("-I$(S)/custom/py3", nodes[0]["cmds"][0]["cmd_args"])
                for node in graph["graph"]:
                    self.assertFalse(any(out.startswith("$(B)/tools/unused") for out in node["outputs"]))
                    self.assertFalse(any(out.endswith(".py2.cpp") for out in node["outputs"]))

    def test_both_variants_reuse_construction_in_either_root_order(self):
        for order in ("py3 py2", "py2 py3"):
            with self.subTest(order=order):
                files = construction_files()
                files.update({
                    "roots/ya.make": "LIBRARY()\n" + lib.BARE_MODULE + f"END()\nRECURSE(../lib {order})\n",
                    "roots/py3/ya.make": "PY3_PROGRAM()\n" + lib.BARE_MODULE
                        + "NO_CHECK_IMPORTS()\nPEERDIR(lib)\nPY_SRCS(MAIN main.py)\nEND()\n",
                    "roots/py2/ya.make": "PY2_PROGRAM()\n" + lib.BARE_MODULE
                        + "NO_PYTHON_INCLUDES()\nNO_CHECK_IMPORTS()\nPEERDIR(lib)\nPY_SRCS(MAIN main.py)\nEND()\n",
                    "roots/py3/main.py": "",
                    "roots/py2/main.py": "",
                })
                graph = lib.make(files, "roots")
                nodes = cython_nodes(graph)
                self.assertEqual(len(nodes), 2)
                for node in nodes:
                    self.assertIn("$(S)/custom/py2/selected/types.pxd", node["inputs"])
                    self.assertNotIn("$(S)/custom/py3/selected/types.pxd", node["inputs"])
                outputs = [out for node in graph["graph"] for out in node["outputs"]]
                self.assertEqual(len(outputs), len(set(outputs)))
                self.assertFalse(any(out.startswith("$(B)/tools/unused") for out in outputs))
                lib.node_by_output(graph, "$(B)/roots/py3/py3")
                lib.node_by_output(graph, "$(B)/roots/py2/py2")


if __name__ == "__main__":
    unittest.main(verbosity=2)
