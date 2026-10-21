import unittest

import lib
from test_python_cython import python_base


class CythonSourceOwnershipTest(unittest.TestCase):
    def test_peer_directory_definitions_own_shared_source_resolution(self):
        producer = "RUN_PROGRAM(tools/gen IN shared/bridge.pxd OUT unselected.o)\n"
        for selector, old_input, expected in (
            ("", producer, "old"),
            ("ONLY_TAGS(PY3)\n", producer, "new"),
            ("EXCLUDE_TAGS(PY2)\n", producer, "new"),
            ("", "", "new"),
        ):
            with self.subTest(selector=selector, old_input=old_input):
                self.check_source_owner(selector, old_input, expected)

    def test_source_statements_share_first_owner_and_no_parse_does_not(self):
        for body, expected in (
            ("BUILDWITH_CYTHON_CPP(early.pyx)\n", "old"),
            ("PY_SRCS(early.pyx)\n", "old"),
            ("RUN_PYTHON3(script.py IN shared/bridge.pxd OUT unselected.o)\n", "old"),
            ("SRCS(shared/bridge.pxd)\n", "old"),
            ("RUN_PROGRAM(tools/gen IN_NOPARSE shared/bridge.pxd OUT unselected.o)\n", "new"),
        ):
            with self.subTest(body=body):
                self.check_source_owner("", body, expected)

    def check_source_owner(self, selector, old_input, expected):
        files = python_base()
        files.update({
            "m/ya.make": "LIBRARY()\n" + lib.BARE_MODULE
                + "SET(PYTHON3 yes)\nADDINCL(FOR cython custom/new)\n"
                + "PEERDIR(proxy)\nBUILDWITH_CYTHON_CPP(main.pyx)\nEND()\n",
            "m/main.pyx": "from shared.bridge cimport value\n",
            "proxy/ya.make": "PY23_LIBRARY()\n" + lib.BARE_MODULE
                + "NO_PYTHON_INCLUDES()\n" + selector + "IF (MODULE_TAG == PY2)\n"
                + "PEERDIR(runtime/old)\nELSE()\nPEERDIR(runtime/new)\nENDIF()\nEND()\n",
            "runtime/old/ya.make": "LIBRARY()\n" + lib.BARE_MODULE
                + "ADDINCL(FOR cython custom/old)\n"
                + old_input
                + "END()\n",
            "runtime/old/early.pyx": "from shared.bridge cimport value\n",
            "runtime/old/early.pxd": "",
            "runtime/old/script.py": "",
            "runtime/new/ya.make": "LIBRARY()\n" + lib.BARE_MODULE + "END()\n",
            "shared/bridge.pxd": "from selected.types cimport value\n",
            "custom/old/selected/types.pxd": "",
            "custom/new/selected/types.pxd": "",
        })
        lib.tool_program(files, "tools/gen", "gen")
        graph = lib.make(files, "m", "--sandboxing")
        cython = lib.node_by_output(graph, "$(B)/m/main.pyx.cpp")
        self.assertIn("-I$(S)/custom/new", cython["cmds"][0]["cmd_args"])
        self.assertIn(f"$(S)/custom/{expected}/selected/types.pxd", cython["inputs"])
        other = "new" if expected == "old" else "old"
        self.assertNotIn(f"$(S)/custom/{other}/selected/types.pxd", cython["inputs"])
        self.assertFalse(any(
            output.startswith(("$(B)/runtime/old/", "$(B)/tools/gen/"))
            for node in graph["graph"] for output in node["outputs"]
        ))


if __name__ == "__main__":
    unittest.main(verbosity=2)
