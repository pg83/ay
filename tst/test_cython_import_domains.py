import unittest

import lib
from test_python_cython import CYTHON, CYTHON_UTILITY, python_base


def fixture(declarations, imports="from . cimport stream\n", mode="CYTHON_C"):
    files = python_base()
    files.update({
        "lib/ya.make": "PY3_LIBRARY()\n" + declarations
            + f"PY_SRCS({mode} readers/packet.pyx)\nEND()\n",
        "lib/readers/packet.pyx": imports,
        "lib/readers/stream.pxd": "cdef int stream\n",
        "native/decoy/__init__.pxd": "from algebra cimport blas, lapack\n",
        "typed/algebra/blas.pxd": "cdef int blas\n",
        "typed/algebra/lapack.pxd": "cdef int lapack\n",
        "typed/pkg/__init__.pxd": "cdef int package\n",
        "typed/pkg/child.pxd": "cdef int child\n",
        "typed/module.pxd": "cdef int attribute\n",
        "typed/module/attribute.pxd": "cdef int wrong_attribute_file\n",
        "native/ext.h": "#include <nested.h>\n",
        "native/nested.h": "",
        "peer/ya.make": "LIBRARY()\n" + lib.BARE_MODULE
            + "ADDINCL(GLOBAL FOR cython typed)\nEND()\n",
    })
    return files


class CythonImportDomainsTest(unittest.TestCase):
    def generated(self, files, extension="c"):
        graph = lib.make(files, "lib", "--sandboxing")
        return graph, lib.node_by_output(graph, "$(B)/lib/readers/packet.pyx." + extension)

    def assert_cython_inputs(self, node, expected):
        self.assertCountEqual(node["inputs"], [
            CYTHON,
            *("$(S)/contrib/tools/cython/Cython/Utility/" + name for name in CYTHON_UTILITY),
            *expected,
        ])

    def test_untyped_native_directory_does_not_supply_package_init(self):
        for mode, extension in (("CYTHON_C", "c"), ("CYTHON_CPP", "cpp")):
            with self.subTest(mode=mode):
                graph, node = self.generated(fixture(
                    "ADDINCL(native/decoy FOR cython typed)\n", mode=mode), extension)
                self.assert_cython_inputs(node, {
                    "$(S)/lib/readers/packet.pyx", "$(S)/lib/readers/stream.pxd",
                })
                self.assertIn("$(S)/contrib/tools/cython/cython.py", node["inputs"])
                self.assertIn("-I$(S)/typed", node["cmds"][0]["cmd_args"])
                self.assertNotIn("-I$(S)/native/decoy", node["cmds"][0]["cmd_args"])
                cc = next(n for n in graph["graph"] if n["kv"].get("p") == "CC"
                          and node["outputs"][0] in n["inputs"])
                self.assertIn(node["uid"], cc["deps"])
                self.assertIn(node["outputs"][0], cc["inputs"])
                self.assertIn("-I$(S)/native/decoy", cc["cmds"][0]["cmd_args"])

    def test_typed_directory_retains_package_init_and_transitive_modules(self):
        _, node = self.generated(fixture(
            "ADDINCL(FOR cython native/decoy FOR cython typed)\n"))
        self.assert_cython_inputs(node, {
            "$(S)/lib/readers/packet.pyx", "$(S)/lib/readers/stream.pxd",
            "$(S)/native/decoy/__init__.pxd", "$(S)/typed/algebra/blas.pxd",
            "$(S)/typed/algebra/lapack.pxd",
        })

    def test_explicit_import_retains_one_module_without_untyped_package_init(self):
        _, node = self.generated(fixture(
            "ADDINCL(native/decoy FOR cython typed)\n",
            "from . cimport stream\nfrom algebra.lapack cimport lapack\n"))
        self.assert_cython_inputs(node, {
            "$(S)/lib/readers/packet.pyx", "$(S)/lib/readers/stream.pxd",
            "$(S)/typed/algebra/lapack.pxd",
        })

    def test_package_child_and_module_attribute_have_distinct_lookup(self):
        _, node = self.generated(fixture("ADDINCL(FOR cython typed)\n",
            "from pkg cimport child\nfrom module cimport attribute\n"))
        self.assert_cython_inputs(node, {
            "$(S)/lib/readers/packet.pyx", "$(S)/typed/pkg/__init__.pxd",
            "$(S)/typed/pkg/child.pxd", "$(S)/typed/module.pxd",
        })

    def test_typed_peer_paths_remain_available(self):
        _, node = self.generated(fixture("PEERDIR(peer)\n", "cimport algebra.blas\n"))
        self.assert_cython_inputs(node, {
            "$(S)/lib/readers/packet.pyx", "$(S)/typed/algebra/blas.pxd",
        })

    def test_cdef_headers_remain_induced_compiler_inputs(self):
        graph, node = self.generated(fixture("ADDINCL(native)\n",
            'cdef extern from "ext.h":\n    pass\n'))
        self.assert_cython_inputs(node, {"$(S)/lib/readers/packet.pyx"})
        cc = next(n for n in graph["graph"] if n["kv"].get("p") == "CC"
                  and node["outputs"][0] in n["inputs"])
        for name in ("ext.h", "nested.h"):
            self.assertNotIn("$(S)/native/" + name, node["inputs"])
            self.assertIn("$(S)/native/" + name, cc["inputs"])

    def test_generic_generator_input_uses_the_registered_cython_language(self):
        files = fixture("ADDINCL(native/decoy FOR cython typed)\n"
            "RUN_PROGRAM(tools/consume IN readers/packet.pyx OUT consumed.cpp)\n"
            "SRCS(${BINDIR}/consumed.cpp)\n")
        lib.tool_program(files, "tools/consume", "consume")
        graph, node = self.generated(files)
        expected = {"$(S)/lib/readers/packet.pyx", "$(S)/lib/readers/stream.pxd"}
        self.assert_cython_inputs(node, expected)
        consumer = lib.node_by_output(graph, "$(B)/lib/consumed.cpp")
        self.assertCountEqual(consumer["inputs"], ["$(B)/tools/consume/consume", *expected])

    def test_native_only_generator_resolves_pxd_with_typed_directories(self):
        files = fixture("")
        lib.tool_program(files, "tools/consume", "consume")
        files["lib/ya.make"] = "LIBRARY()\n" + lib.BARE_MODULE \
            + "ADDINCL(native/decoy FOR cython typed)\n" \
            + "RUN_PROGRAM(tools/consume IN readers/bridge.pxd OUT consumed.cpp)\nEND()\n"
        files["lib/readers/bridge.pxd"] = "from . cimport stream\ncimport algebra.lapack\n"
        graph = lib.make(files, "lib", "--sandboxing")
        consumer = lib.node_by_output(graph, "$(B)/lib/consumed.cpp")
        self.assertCountEqual(consumer["inputs"], {
            "$(B)/tools/consume/consume",
            "$(S)/lib/readers/bridge.pxd", "$(S)/lib/readers/stream.pxd",
            "$(S)/typed/algebra/lapack.pxd",
        })

    def test_generated_includes_keep_the_producer_main_output_and_real_inputs(self):
        for declared in ("", " OUTPUT_INCLUDES algebra/lapack.pxd"):
            with self.subTest(declared=declared):
                files = fixture("ADDINCL(native/decoy FOR cython typed)\nPEERDIR(gen)\n",
                    'include "gen/generated.pxi"\n')
                lib.tool_program(files, "tools/generate", "generate")
                files["gen/ya.make"] = "GEN_LIBRARY()\nRUN_PROGRAM(tools/generate " \
                    "IN seed.txt OUT_NOAUTO marker.bin generated.pxi" + declared + ")\nEND()\n"
                files["gen/seed.txt"] = "physical generator input\n"
                graph, node = self.generated(files)
                producer = lib.node_by_output(graph, "$(B)/gen/generated.pxi")
                self.assertEqual(producer["outputs"], [
                    "$(B)/gen/marker.bin", "$(B)/gen/generated.pxi",
                ])
                self.assertIn("$(S)/gen/seed.txt", producer["inputs"])
                self.assertIn("$(B)/tools/generate/generate", producer["inputs"])
                self.assertIn(producer["uid"], node["deps"])
                for output in producer["outputs"]:
                    self.assertIn(output, node["inputs"])
                self.assertNotIn("$(S)/gen/generated.pxi", node["inputs"])
                self.assertEqual("$(S)/typed/algebra/lapack.pxd" in node["inputs"], bool(declared))
                expected = {"$(S)/lib/readers/packet.pyx", *producer["outputs"]}
                if declared:
                    expected.add("$(S)/typed/algebra/lapack.pxd")
                self.assert_cython_inputs(node, expected)

    def test_no_resolve_suppresses_import_resolution(self):
        files = fixture("SET(DONT_RESOLVE_INCLUDES yes)\n"
            "ADDINCL(FOR cython native/decoy FOR cython typed)\n")
        _, node = self.generated(files)
        self.assert_cython_inputs(node, {"$(S)/lib/readers/packet.pyx"})


if __name__ == "__main__":
    unittest.main(verbosity=2)
