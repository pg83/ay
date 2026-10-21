import unittest

import lib
from test_go_flatbuffers import files as go_files
from test_proto_flatc import flatc_files


BARE = "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"


def include_flags(node):
    args = node["cmds"][0]["cmd_args"]
    return [args[i + 1] for i, arg in enumerate(args[:-1]) if arg == "-I"]


class FlatcAddinclTest(unittest.TestCase):
    def test_local_flatc_paths_drive_command_and_schema_resolution(self):
        files = flatc_files()
        files.update({
            "f/ya.make": BARE + "ADDINCL(FOR flatc schemas)\nSRCS(packet.fbs use.cpp)\nEND()\n",
            "f/packet.fbs": 'include "only.fbs";\ntable Packet {}\n',
            "f/use.cpp": "int use;\n",
            "schemas/only.fbs": "table Only {}\n",
            "f/only.fbs.h": "#pragma once\n",
        })
        graph = lib.make(files, "f")
        flatc = lib.node_by_output(graph, "$(B)/f/packet.fbs.h")
        self.assertEqual(include_flags(flatc), ["$(B)", "$(S)", "$(S)/schemas"])
        args = flatc["cmds"][0]["cmd_args"]
        self.assertEqual(args[args.index("-I"):args.index("-o")], ["-I", "$(B)", "-I", "$(S)", "-I", "$(S)/schemas"])
        self.assertIn("$(S)/schemas/only.fbs", flatc["inputs"])
        compile_node = lib.node_by_output(graph, "$(B)/f/use.cpp.o")
        self.assertNotIn("$(S)/schemas", compile_node["cmds"][0]["cmd_args"])

    def test_global_flatc_paths_propagate_without_private_paths_or_duplicates(self):
        files = flatc_files()
        files.update({
            "peer/ya.make": BARE + "ADDINCL(GLOBAL FOR flatc common)\nADDINCL(FOR flatc private)\nSRCS(peer.cpp)\nEND()\n",
            "peer/peer.cpp": "int peer;\n",
            "middle/ya.make": BARE + "PEERDIR(peer)\nSRCS(middle.cpp)\nEND()\n",
            "middle/middle.cpp": "int middle;\n",
            "f/ya.make": BARE + "PEERDIR(middle peer)\nADDINCL(FOR flatc own)\nSRCS(packet.fbs use.cpp)\nEND()\n",
            "f/packet.fbs": 'include "choice.fbs";\ntable Packet {}\n',
            "f/use.cpp": "int use;\n",
            "own/choice.fbs": "table Own {}\n",
            "f/choice.fbs.h": "#pragma once\n",
            "common/choice.fbs": "table Common {}\n",
            "private/private.fbs": "table Private {}\n",
        })
        graph = lib.make(files, "f")
        flatc = lib.node_by_output(graph, "$(B)/f/packet.fbs.h")
        self.assertEqual(include_flags(flatc), ["$(B)", "$(S)", "$(S)/own", "$(S)/common"])
        self.assertIn("$(S)/own/choice.fbs", flatc["inputs"])
        self.assertNotIn("$(S)/common/choice.fbs", flatc["inputs"])
        compile_node = lib.node_by_output(graph, "$(B)/f/use.cpp.o")
        for directory in ("own", "common", "private"):
            self.assertNotIn(f"$(S)/{directory}", compile_node["cmds"][0]["cmd_args"])

    def test_default_language_paths_resolve_without_entering_flatc_command(self):
        files = flatc_files()
        files.update({
            "f/ya.make": BARE + "ADDINCL(schemas)\nSRCS(packet.fbs)\nEND()\n",
            "f/packet.fbs": 'include "only.fbs";\ntable Packet {}\n',
            "schemas/only.fbs": "table Only {}\n",
            "schemas/only.fbs.h": "#pragma once\n",
        })
        graph = lib.make(files, "f")
        flatc = lib.node_by_output(graph, "$(B)/f/packet.fbs.h")
        self.assertEqual(include_flags(flatc), ["$(B)", "$(S)"])
        self.assertIn("$(S)/schemas/only.fbs", flatc["inputs"])

    def test_flatc64_keeps_fixed_command_roots_and_uses_flatc_resolution_paths(self):
        files = flatc_files()
        files.update({
            "f/ya.make": BARE + "ADDINCL(FOR flatc schemas)\nSRCS(packet.fbs64)\nEND()\n",
            "f/packet.fbs64": 'include "only.fbs64";\ntable Packet {}\n',
            "schemas/only.fbs64": "table Only {}\n",
            "f/only.fbs64.h": "#pragma once\n",
        })
        graph = lib.make(files, "f")
        flatc = lib.node_by_output(graph, "$(B)/f/packet.fbs64.h")
        self.assertEqual(include_flags(flatc), ["$(S)", "$(B)"])
        self.assertIn("$(S)/schemas/only.fbs64", flatc["inputs"])

    def test_go_flatc_uses_language_specific_include_paths(self):
        files = go_files()
        files.update({
            "f/ya.make": "FBS_LIBRARY()\nONLY_TAGS(GO_FBS)\nADDINCL(FOR flatc schemas)\nSRCS(packet.fbs)\nEND()\n",
            "f/packet.fbs": 'include "only.fbs";\ntable Packet {}\n',
            "schemas/only.fbs": "table Only {}\n",
            "f/only.fbs.h": "#pragma once\n",
        })
        graph = lib.make(files, "f")
        flatc = lib.node_by_output(graph, "$(B)/f/packet.fbs.gosrc")
        self.assertEqual(include_flags(flatc), ["$(B)", "$(S)", "$(S)/schemas"])
        args = flatc["cmds"][0]["cmd_args"]
        self.assertEqual(args[args.index("-I"):args.index("-o")], ["-I", "$(B)", "-I", "$(S)", "-I", "$(S)/schemas"])
        self.assertIn("$(S)/schemas/only.fbs", flatc["inputs"])


if __name__ == "__main__":
    unittest.main()
