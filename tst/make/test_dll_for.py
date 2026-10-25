import unittest

from tst import lib


BARE = "NO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"


class DllForTest(unittest.TestCase):
    def test_inherited_module_body_uses_source_directory_and_dll_scope(self):
        files = {
            "native/ya.make": (
                f"LIBRARY(ignored_name)\n{BARE}"
                "PEERDIR(dependency)\nSRCS(source.cpp)\nCFLAGS(-DINHERITED)\n"
                "IF (DLL_FOR)\nCFLAGS(-DFOR_DLL)\nENDIF()\nEND()\n"
            ),
            "native/source.cpp": '#include "header.h"\nint value;\n',
            "native/header.h": "#pragma once\n",
            "dependency/ya.make": f"LIBRARY()\n{BARE}SRCS(dependency.cpp)\nEND()\n",
            "dependency/dependency.cpp": "int dependency;\n",
            "shared/ya.make": "DLL_FOR(native renamed EXPORTS api.exports)\n",
            "shared/api.exports": "C value\n",
        }
        graph = lib.make(files, "shared")
        link = lib.node_by_output(graph, "$(B)/shared/librenamed.so")
        compiled = next(node for node in graph["graph"] if node["kv"].get("p") == "CC" and "$(S)/native/source.cpp" in node["inputs"])
        args = compiled["cmds"][0]["cmd_args"]
        self.assertIn("-DINHERITED", args)
        self.assertIn("-DFOR_DLL", args)
        self.assertIn("-fPIC", args)
        self.assertIn("-I$(S)/native", args)
        self.assertNotIn("-I$(B)/native", args)
        self.assertIn("$(S)/native/header.h", compiled["inputs"])
        self.assertIn("$(S)/shared/api.exports", link["inputs"])
        self.assertIn("$(B)/dependency/libdependency.a", link["inputs"])
        self.assertIn(compiled["uid"], link["deps"])
        self.assertFalse(any("$(B)/native/libignored_name.a" in node["outputs"] for node in graph["graph"]))

    def test_module_arguments_stay_literal_and_inactive_branches_are_not_loaded(self):
        files = {
            "native/ya.make": f"LIBRARY()\n{BARE}SRCS(source.cpp)\nEND()\n",
            "native/source.cpp": "int source;\n",
            "shared/ya.make": (
                "SET(NAME renamed)\nIF (OS_LINUX)\nDLL_FOR(native $NAME)\n"
                "ELSE()\nDLL_FOR(not_in_this_slice ignored)\nENDIF()\n"
            ),
        }
        graph = lib.make(files, "shared")
        link = lib.node_by_output(graph, "$(B)/shared/lib$NAME.so")
        compiled = next(n for n in graph["graph"] if "$(S)/native/source.cpp" in n["inputs"])
        self.assertIn(compiled["uid"], link["deps"])

    def test_source_directory_argument_is_literal(self):
        result = lib.make_process({
            "native/ya.make": f"LIBRARY()\n{BARE}END()\n",
            "shared/ya.make": "SET(BASE native)\nDLL_FOR($BASE renamed)\n",
        }, "shared")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("$BASE/ya.make", result.stderr)

    def test_recursed_dll_for_is_discovered_in_test_mode(self):
        graph = lib.make({
            "native/ya.make": f"LIBRARY()\n{BARE}SRCS(source.cpp)\nEND()\n",
            "native/source.cpp": "int source;\n",
            "app/ya.make": f"PROGRAM()\n{BARE}SRCS(main.cpp)\nEND()\nRECURSE(shared)\n",
            "app/main.cpp": "int main() { return 0; }\n",
            "app/shared/ya.make": "DLL_FOR(native wrapped)\n",
        }, "app", "-t")
        link = lib.node_by_output(graph, "$(B)/app/shared/libwrapped.so")
        self.assertIn(link["uid"], graph["result"])

    def test_inherited_recurse_is_relative_to_the_dll_directory(self):
        graph = lib.make({
            "native/ya.make": f"LIBRARY()\n{BARE}SRCS(source.cpp)\nEND()\nRECURSE(helper)\n",
            "native/source.cpp": "int source;\n",
            "shared/ya.make": "DLL_FOR(native wrapped)\n",
            "shared/helper/ya.make": f"PROGRAM()\n{BARE}SRCS(main.cpp)\nEND()\n",
            "shared/helper/main.cpp": "int main() { return 0; }\n",
        }, "shared")
        helper = lib.node_by_output(graph, "$(B)/shared/helper/helper")
        self.assertIn(helper["uid"], graph["result"])

    def test_self_reference_is_rejected(self):
        result = lib.make_process({"shared/ya.make": "DLL_FOR(shared renamed)\n"}, "shared")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("DLL_FOR", result.stderr)
        self.assertIn("shared", result.stderr)
        self.assertIn("current directory", result.stderr)


if __name__ == "__main__":
    unittest.main(verbosity=2)
