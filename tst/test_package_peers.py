import unittest

import lib


class PackagePeersTest(unittest.TestCase):
    def test_shared_libraries_are_collected_as_linked_outputs(self):
        for module in ("CI_GROUP", "PACKAGE"):
            with self.subTest(module=module):
                files = {
                    "m/ya.make": f"{module}()\nPEERDIR(shared)\nEND()\n",
                    "shared/ya.make": f"DLL(shared)\n{lib.BARE_MODULE}ALLOCATOR(FAKE)\n"
                        "PEERDIR(static)\nSRCS(shared.cpp)\nEND()\n",
                    "shared/shared.cpp": "int shared;\n",
                    "static/ya.make": f"LIBRARY()\n{lib.BARE_MODULE}SRCS(static.cpp)\nEND()\n",
                    "static/static.cpp": "int value;\n",
                }
                lib.tool_program(files, "tools/fix_elf", "fix_elf")
                graph = lib.make(files, "m", "--sandboxing")
                output = "$(B)/m/m.ci.pkg.fake" if module == "CI_GROUP" else "$(B)/m/m.final.pkg.fake"
                package = lib.node_by_output(graph, output)
                shared = lib.node_by_output(graph, "$(B)/shared/libshared.so")
                archive = lib.node_by_output(graph, "$(B)/static/libstatic.a")
                self.assertIn(shared["outputs"][0], package["inputs"])
                self.assertIn(shared["uid"], package["deps"])
                self.assertNotIn(archive["outputs"][0], package["inputs"])
                self.assertIn(archive["uid"], shared["deps"])
                if module == "PACKAGE":
                    self.assertIn("$(B)/m/shared/libshared.so", package["outputs"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
