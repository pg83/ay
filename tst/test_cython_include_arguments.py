"""Cython local/system include paths form an ordered set; peer paths stay separate."""
import unittest

import lib
from test_python_cython import python_base


SYSTEM = "contrib/tools/cython/Cython/Includes"
CASES = {
    "overlap": ("ADDINCL(FOR cython amber FOR cython indigo FOR cython amber)\n", ["amber", "indigo", SYSTEM]),
    "reverse": ("ADDINCL(FOR cython indigo FOR cython amber FOR cython indigo)\n", ["indigo", "amber", SYSTEM]),
    "empty": ("", [SYSTEM]),
    "system": (f"ADDINCL(FOR cython {SYSTEM} FOR cython amber)\n", [SYSTEM, "amber"]),
    "peer": ("ADDINCL(FOR cython amber)\nPEERDIR(peer)\n", ["amber", SYSTEM, "amber", "indigo"]),
}


def files(declaration, bare=False):
    result = python_base()
    result["maple/ya.make"] = "LIBRARY()\n" + ("" if bare else lib.BARE_MODULE) + "USE_PYTHON3()\n" + declaration + "BUILDWITH_CYTHON_CPP(code.pyx --module-name repeated --module-name repeated)\nEND()\n"
    result["maple/code.pyx"] = "cdef int x\n"
    result["amber/.keep"] = ""
    result["indigo/.keep"] = ""
    result[SYSTEM + "/.keep"] = ""
    result["peer/ya.make"] = "LIBRARY()\n" + lib.BARE_MODULE + "ADDINCL(GLOBAL FOR cython amber GLOBAL FOR cython indigo)\nEND()\n"
    result["library/python/runtime_py3/ya.make"] = "LIBRARY()\n" + lib.BARE_MODULE + "END()\n"
    return result


class CythonIncludeArgumentsTest(unittest.TestCase):
    def test_ordered_local_and_system_paths_preserve_peer_boundary(self):
        for name, (declaration, expected) in CASES.items():
            with self.subTest(name=name):
                graph = lib.make(files(declaration), "maple")
                cy = lib.node_by_output(graph, "$(B)/maple/code.pyx.cpp")
                args = cy["cmds"][0]["cmd_args"]
                self.assertEqual([a for a in args if a.startswith("-I")], ["-I$(B)", "-I$(S)"] + ["-I$(S)/" + path for path in expected])
                self.assertEqual(args.count("--module-name"), 2)
                self.assertEqual(args.count("repeated"), 2)
                self.assertIn("$(S)/maple/code.pyx", cy["inputs"])
                cc = lib.node_by_output(graph, "$(B)/maple/code.pyx.cpp.o")
                self.assertIn(cy["uid"], cc["deps"])
                self.assertIn(cy["outputs"][0], cc["inputs"])

    def test_bare_context_keeps_same_ordered_include_policy(self):
        graph = lib.make(files("ADDINCL(FOR cython amber FOR cython amber)\n", bare=True), "maple")
        args = lib.node_by_output(graph, "$(B)/maple/code.pyx.cpp")["cmds"][0]["cmd_args"]
        self.assertEqual([a for a in args if a.startswith("-I")], ["-I$(B)", "-I$(S)", "-I$(S)/amber", "-I$(S)/" + SYSTEM])


if __name__ == "__main__":
    unittest.main(verbosity=2)
