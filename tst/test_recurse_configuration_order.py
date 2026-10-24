import collections
import unittest

import lib
from test_cython_sdk_scope import fixture as sdk_fixture


def fixture(recurses):
    files = sdk_fixture("consumer-only")
    for name in list(files):
        if name.startswith(("maple/", "grove/")):
            del files[name]
    files.update({
        "forest/ya.make": "LIBRARY(Forest)\n" + lib.BARE_MODULE
            + "SRCS(forest.cpp)\nEND()\nRECURSE(" + recurses + ")\n",
        "forest/forest.cpp": "int forest;\n",
        "forest/branch/ya.make": "LIBRARY(Branch)\n" + lib.BARE_MODULE
            + "SRCS(branch.cpp)\nEND()\nRECURSE(leaf)\n",
        "forest/branch/branch.cpp": "int branch;\n",
        "forest/branch/leaf/ya.make": "PROGRAM(Maple)\n" + lib.BARE_MODULE
            + "USE_PYTHON3()\nBUILDWITH_CYTHON_CPP(main.pyx)\nEND()\n",
        "forest/branch/leaf/main.pyx": "from shared.bridge cimport value\n",
        "forest/cedar/ya.make": "PROGRAM(Birch)\n" + lib.BARE_MODULE
            + "USE_PYTHON2()\nBUILDWITH_CYTHON_CPP(main.pyx)\nEND()\n",
        "forest/cedar/main.pyx": "from shared.bridge cimport value\n",
    })
    lib.tool_program(files, "tools/fix_elf", "fix_elf")
    return files


class RecurseConfigurationOrderTest(unittest.TestCase):
    def test_shallower_module_configures_before_deeper_sibling(self):
        for recurses in ("branch cedar", "cedar branch", "branch cedar branch"):
            with self.subTest(recurses=recurses):
                graph = lib.make(fixture(recurses), "forest", "--sandboxing",
                                 "--target-platform", "default-linux-x86_64",
                                 "--add-modules-to-results")
                nodes = {node["uid"]: node for node in graph["graph"]}
                self.assertEqual(len(nodes), len(graph["graph"]))
                self.assertTrue(graph["result"])
                self.assertLessEqual(set(graph["result"]), nodes.keys())
                self.assertTrue(all(set(node["deps"]) <= nodes.keys() for node in nodes.values()))
                counts = collections.Counter(out for node in nodes.values() for out in node["outputs"])
                for directory, stem, suffix in (("forest/branch/leaf", "Maple", ""),
                                                ("forest/cedar", "Birch", ".py2")):
                    source = f"$(S)/{directory}/main.pyx"
                    output = f"$(B)/{directory}/main.pyx{suffix}.cpp"
                    generated = lib.node_by_output(graph, output)
                    self.assertCountEqual(
                        [name for name in generated["inputs"] if "/Includes/libcpp/" in name],
                        [f"$(S)/contrib/tools/cython_py2/Cython/Includes/libcpp/{name}"
                         for name in ("pair.pxd", "utility.pxd")],
                    )
                    self.assertIn(source, generated["inputs"])
                    self.assertIn("$(S)/shared/bridge.pxd", generated["inputs"])
                    compiled = lib.node_by_output(graph, output + ".o")
                    self.assertIn(generated["uid"], compiled["deps"])
                    self.assertIn(output, compiled["inputs"])
                    program = lib.node_by_output(graph, f"$(B)/{directory}/{stem}")
                    self.assertIn(compiled["uid"], program["deps"])
                    self.assertIn(program["uid"], graph["result"])
                    self.assertEqual(counts[output], 1)
                    self.assertEqual(counts[program["outputs"][0]], 1)
                archive = lib.node_by_output(graph, "$(B)/forest/libForest.a")
                self.assertIn(archive["uid"], graph["result"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
