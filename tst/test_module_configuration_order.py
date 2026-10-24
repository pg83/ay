import collections
import unittest

import lib
from test_cython_sdk_scope import fixture


def check_graph(test, graph):
    ids = {node["uid"] for node in graph["graph"]}
    test.assertTrue(graph["result"])
    test.assertLessEqual(set(graph["result"]), ids)
    test.assertTrue(all(set(node["deps"]) <= ids for node in graph["graph"]))


class ModuleConfigurationOrderTest(unittest.TestCase):
    def test_first_configured_source_owner_follows_root_order(self):
        files = fixture("consumer-only")
        files["maple/ya.make"] = files["maple/ya.make"].replace("PEERDIR(grove)\n", "")
        files["birch/ya.make"] = (
            "DLL(Birch)\n" + lib.BARE_MODULE
            + "USE_PYTHON2()\nBUILDWITH_CYTHON_CPP(early.pyx)\nEND()\n"
        )
        files["birch/early.pyx"] = "from shared.bridge cimport value\n"
        lib.tool_program(files, "tools/fix_elf", "fix_elf")
        for roots, selected in ((["maple", "birch"], "cython"),
                                (["birch", "maple"], "cython_py2")):
            with self.subTest(roots=roots):
                graph = lib.make(files, roots[-1], *roots[:-1], "--sandboxing",
                                 "--target-platform", "default-linux-x86_64",
                                 "--add-modules-to-results")
                check_graph(self, graph)
                for output, sdk in (("$(B)/maple/main.pyx.cpp", selected),
                                    ("$(B)/birch/early.pyx.py2.cpp", "cython_py2")):
                    generated = lib.node_by_output(graph, output)
                    self.assertCountEqual(
                        [path for path in generated["inputs"] if "/Includes/libcpp/" in path],
                        [f"$(S)/contrib/tools/{sdk}/Cython/Includes/libcpp/{name}"
                         for name in ("pair.pxd", "utility.pxd")],
                    )
                result_nodes = [node for node in graph["graph"] if node["uid"] in graph["result"]]
                self.assertTrue(any("$(B)/maple/libMaple.a" in node["outputs"] for node in result_nodes))
                self.assertTrue(any("$(B)/birch/libBirch.so" in node["outputs"] for node in result_nodes))

    def test_unconsumed_peer_commands_do_not_become_module_results(self):
        for case, sdk in (("old-cython", "cython_py2"), ("new-cython", "cython")):
            with self.subTest(case=case):
                graph = lib.make(fixture(case), "maple", "--sandboxing",
                                 "--target-platform", "default-linux-x86_64",
                                 "--add-modules-to-results")
                check_graph(self, graph)
                generated = lib.node_by_output(graph, "$(B)/maple/main.pyx.cpp")
                self.assertCountEqual(
                    [path for path in generated["inputs"] if "/Includes/libcpp/" in path],
                    [f"$(S)/contrib/tools/{sdk}/Cython/Includes/libcpp/{name}"
                     for name in ("pair.pxd", "utility.pxd")],
                )
                self.assertFalse(any(
                    output.startswith("$(B)/grove/")
                    for node in graph["graph"] for output in node["outputs"]
                ))
                self.assertEqual(
                    [node["outputs"] for node in graph["graph"] if node["uid"] in graph["result"]],
                    [["$(B)/maple/libMaple.a"]],
                )

    def test_later_consumer_reuses_nonleaf_outputs_and_copied_objects(self):
        files = {
            "forest/ya.make": "LIBRARY()\n" + lib.BARE_MODULE + "PEERDIR(leaf)\nSRCS(forest.cpp)\nEND()\n",
            "forest/forest.cpp": "int forest;\n",
            "leaf/ya.make": "LIBRARY()\n" + lib.BARE_MODULE
                + "PEERDIR(twig)\nCOPY_FILE(seed.bin copied.o)\nSRCS(leaf.cpp)\nEND()\n",
            "leaf/leaf.cpp": "int leaf;\n",
            "leaf/seed.bin": "object fixture\n",
            "twig/ya.make": "LIBRARY()\n" + lib.BARE_MODULE
                + "JOIN_SRCS(joined.cpp left.cpp right.cpp)\nEND()\n",
            "twig/left.cpp": "int left;\n",
            "twig/right.cpp": "int right;\n",
            "branch/ya.make": "PROGRAM(branch)\n" + lib.BARE_MODULE
                + "PEERDIR(forest)\nSRCS(main.cpp)\nEND()\n",
            "branch/main.cpp": "int main(){return 0;}\n",
        }
        for roots in (("forest", "branch"), ("branch", "forest")):
            with self.subTest(roots=roots):
                graph = lib.make(files, roots[-1], *roots[:-1], "--sandboxing")
                check_graph(self, graph)
                counts = collections.Counter(output for node in graph["graph"] for output in node["outputs"])
                for output in ("$(B)/leaf/copied.o", "$(B)/leaf/libleaf.a", "$(B)/twig/joined.cpp",
                               "$(B)/twig/libtwig.a", "$(B)/forest/libforest.a", "$(B)/branch/branch"):
                    self.assertEqual(counts[output], 1, output)
                copied = lib.node_by_output(graph, "$(B)/leaf/copied.o")
                archive = lib.node_by_output(graph, "$(B)/leaf/libleaf.a")
                self.assertIn("$(S)/leaf/seed.bin", copied["inputs"])
                self.assertIn(copied["uid"], archive["deps"])
                self.assertIn(copied["outputs"][0], archive["inputs"])
                program = lib.node_by_output(graph, "$(B)/branch/branch")
                for output in ("$(B)/forest/libforest.a", "$(B)/leaf/libleaf.a", "$(B)/twig/libtwig.a"):
                    peer = lib.node_by_output(graph, output)
                    self.assertIn(output, program["inputs"])
                    self.assertIn(peer["uid"], program["deps"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
