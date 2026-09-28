import unittest

import lib


STUB_LIBRARY = "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n"


class BundleTest(unittest.TestCase):
    def test_program_and_unresolved_bundle_targets(self):
        files = {
            "library/cpp/resource/ya.make": STUB_LIBRARY,
            "cons/ya.make": (
                "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(c.cpp)\n"
                "BUNDLE(prog NAME p.bin)\n"
                "BUNDLE(prog NAME p.bin)\n"
                "BUNDLE(missing NAME m.bin)\n"
                "BUNDLE(noopener NAME n.bin)\n"
                "BUNDLE(empty NAME e.bin)\n"
                "RESOURCE(p.bin p m.bin m n.bin n e.bin e)\n"
                "END()\n"
            ),
            "cons/c.cpp": "int c(){return 0;}\n",
            "noopener/ya.make": "SRCS(x.cpp)\n",
            "empty/ya.make": "UNION()\nEND()\n",
        }
        lib.tool_program(files, "prog", "prog")
        lib.tool_program(files, "tools/rescompiler", "rescompiler")
        lib.tool_program(files, "tools/rescompressor", "rescompressor")
        graph = lib.make(files, "cons")

        program = lib.node_by_output(graph, "$(B)/prog/prog")
        bundles = [node for node in graph["graph"] if node["kv"].get("p") == "BN"]
        self.assertEqual([node["outputs"] for node in bundles], [
            ["$(B)/cons/p.bin"], ["$(B)/cons/m.bin"], ["$(B)/cons/n.bin"], ["$(B)/cons/e.bin"],
        ])
        resolved = bundles[0]
        self.assertEqual(resolved["cmds"][0]["cmd_args"][1:], [
            "$(S)/build/scripts/fs_tools.py", "rename", "$(B)/prog/prog", "$(B)/cons/p.bin",
        ])
        self.assertEqual(resolved["inputs"], ["$(B)/prog/prog"])
        self.assertEqual(resolved["deps"], [program["uid"]])
        for node, name in zip(bundles[1:], ("m.bin", "n.bin", "e.bin")):
            self.assertEqual(node["cmds"][0]["cmd_args"][1:], [
                "$(S)/build/scripts/fs_tools.py", "rename", f"$(B)/cons/{name}",
            ])
            self.assertEqual(node["inputs"], [])
            self.assertEqual(node.get("deps", []), [])

        objcopy = lib.node_by_output_prefix(graph, "$(B)/cons/objcopy_")
        for node in bundles:
            self.assertIn(node["outputs"][0], objcopy["inputs"])
            self.assertIn(node["uid"], objcopy["deps"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
