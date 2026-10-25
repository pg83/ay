import unittest

from tst import lib
from tst.make.test_go_package import STD, go_module, go_node, go_tool_args, go_world


def files():
    result = go_world()
    result["contrib/libs/flatbuffers/ya.make"] = f"LIBRARY()\n{lib.BARE_MODULE}\nEND()\n"
    for peer in (f"{STD}/bytes", f"{STD}/strconv", "vendor/github.com/google/flatbuffers/go"):
        go_module(result, peer)
    lib.tool_program(result, "contrib/libs/flatbuffers/flatc", "flatc")
    result["build/scripts/postprocess_go_fbs.py"] = ""
    result["build/scripts/tar_sources.py"] = ""
    return result


class GoFlatbuffersTest(unittest.TestCase):
    def test_go_variant_archives_generated_sources_and_adds_runtime_peers(self):
        sources = files()
        sources["schema/packet/ya.make"] = "FBS_LIBRARY()\nONLY_TAGS(GO_FBS)\nGO_PACKAGE_NAME(Packet)\nFLATC_FLAGS(--reflect-names)\nSRCS(packet.fbs)\nEND()\n"
        sources["schema/packet/packet.fbs"] = "namespace Packet;\ntable T { value:int; }\n"
        graph = lib.make(sources, "schema/packet")
        generated = lib.node_by_output(graph, "$(B)/schema/packet/packet.fbs.gosrc")
        self.assertEqual(generated["kv"], {"p": "FG", "pc": "light-green", "tared_kind": "nodir"})
        self.assertEqual(generated["cmds"][0]["cmd_args"], [
            "$(B)/contrib/libs/flatbuffers/flatc/flatc", "--go", "--gen-mutable", "--go-namespace", "Packet", "--reflect-names",
            "-I", "$(B)", "-I", "$(S)", "-o", "$(B)/schema/packet/_generated", "$(S)/schema/packet/packet.fbs",
        ])
        self.assertIn("$(S)/schema/packet/packet.fbs", generated["inputs"])
        self.assertEqual(len(generated["cmds"]), 3)
        self.assertEqual(generated["cmds"][0]["cwd"], "$(B)")
        self.assertNotIn("cwd", generated["cmds"][1])
        self.assertNotIn("cwd", generated["cmds"][2])
        self.assertEqual(generated["cmds"][2]["cmd_args"][2:], [
            "--flat", "--input", "$(B)/schema/packet/_generated", "--output", "$(B)/schema/packet/packet.fbs.gosrc", "--exts", ".go",
        ])
        archive = go_node(graph, "schema/packet")
        self.assertIn("$(B)/schema/packet/packet.fbs.gosrc", go_tool_args(archive))
        self.assertIn(generated["uid"], archive["deps"])
        for peer in (f"{STD}/bytes/bytes.a", f"{STD}/strconv/strconv.a", "vendor/github.com/google/flatbuffers/go/go.a"):
            self.assertIn(peer, go_tool_args(archive))

    def test_go_peer_selects_flatbuffer_variant_and_inherits_namespace_map(self):
        sources = files()
        sources["schema/base/ya.make"] = "FBS_LIBRARY()\nFBS_NAMESPACE(Example.Base)\nSRCS(base.fbs)\nEND()\n"
        sources["schema/base/base.fbs"] = "namespace Example.Base;\ntable T { value:int; }\n"
        sources["schema/packet/ya.make"] = "FBS_LIBRARY()\nFBS_NAMESPACE(Example.Packet custom/path)\nPEERDIR(schema/base schema/middle)\nSRCS(packet.fbs)\nEND()\n"
        sources["schema/packet/packet.fbs"] = 'include "schema/base/base.fbs";\nnamespace Example.Packet;\ntable T { value:int; }\n'
        sources["schema/middle/ya.make"] = "FBS_LIBRARY()\nFBS_NAMESPACE(Example.Middle)\nPEERDIR(schema/base)\nEND()\n"
        go_module(sources, "app", macros="PEERDIR(schema/packet)\n", module="GO_PROGRAM")
        graph = lib.make(sources, "app")
        generated = lib.node_by_output(graph, "$(B)/schema/packet/packet.fbs.gosrc")
        self.assertIn("$(S)/schema/base/base.fbs", generated["inputs"])
        post = generated["cmds"][1]["cmd_args"]
        self.assertEqual(post[post.index("--map") + 1:], ["Example.Base=schema/base", "Example.Middle=schema/middle", "Example.Packet=custom/path"])
        self.assertIn("schema/base/base.a", go_tool_args(go_node(graph, "schema/packet")))
        app = lib.node_by_output(graph, "$(B)/app/app")
        self.assertIn("schema/packet/packet.a", go_tool_args(app))


if __name__ == "__main__":
    unittest.main()
