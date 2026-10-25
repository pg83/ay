import unittest

from tst import lib
from tst.make.test_proto_cpp import proto_files


class ListProtoTest(unittest.TestCase):
    def test_list_keeps_proto_closure_for_peer_generator(self):
        files = proto_files()
        lib.tool_program(files, "generator", "generator")
        files.update({
            "p/ya.make": (
                "PROTO_LIBRARY()\nONLY_TAGS(CPP_PROTO)\nSRCS(a.proto b.proto)\n"
                "LIST_PROTO(TO catalog.proto a.proto b.proto)\nEND()\n"
            ),
            "p/a.proto": 'syntax = "proto3";\nimport "p/b.proto";\n',
            "p/b.proto": 'syntax = "proto3";\n',
            "use/ya.make": (
                "LIBRARY()\nNO_UTIL()\nNO_RUNTIME()\nNO_LIBC()\nPEERDIR(p)\n"
                "RUN_PROGRAM(generator IN p/catalog.proto OUT generated.cpp)\n"
                "SRCS(use.cpp)\nEND()\n"
            ),
            "use/use.cpp": '#include "p/catalog.pb.h"\n',
        })
        graph = lib.make(files, "use")
        listing = lib.node_by_output(graph, "$(B)/p/catalog.proto")
        touch = lib.node_by_output(graph, "$(B)/p/catalog.pb.h")
        generator = lib.node_by_output(graph, "$(B)/use/generated.cpp")
        self.assertEqual(listing["cmds"][0]["cmd_args"][-3:], [
            "$(S)/build/scripts/list.py", "a.proto", "b.proto",
        ])
        self.assertEqual(listing["cmds"][0]["stdout"], "$(B)/p/catalog.proto")
        self.assertEqual(touch["cmds"][0]["cmd_args"][-2:], [
            "$(S)/build/scripts/touch.py", "$(B)/p/catalog.pb.h",
        ])
        self.assertIn(listing["uid"], touch["deps"])
        self.assertIn(listing["uid"], generator["deps"])
        for node in (listing, touch, generator):
            for name in ("a.proto", "b.proto"):
                self.assertIn(f"$(S)/p/{name}", node["inputs"])
        self.assertNotIn("$(S)/build/scripts/list.py", generator["inputs"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
