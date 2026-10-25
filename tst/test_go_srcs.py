import unittest

import lib
from test_go_tests import go_world


def files(declaration):
    sources = go_world()
    sources.update({
        "g/ya.make": f"GO_LIBRARY()\n{lib.BARE_MODULE}ADDINCL(GLOBAL ${{BINDIR}})\nSRCS(g.go)\n{declaration}\nCOPY_FILE(seed.txt generated.h)\nEND()\n",
        "g/g.go": "package g\n",
        "g/ignored.h": '#include "generated.h"\n',
        "g/ignored.hpp": '#include "generated.h"\n',
        "g/seed.txt": "\n",
        "consumer/ya.make": f"GO_LIBRARY()\n{lib.BARE_MODULE}SRCS(consumer.go)\nPEERDIR(g)\nEND()\n",
        "consumer/consumer.go": "package consumer\n",
    })
    return sources


class GoSourcesTest(unittest.TestCase):
    def test_go_srcs_does_not_export_unsupported_header_inputs(self):
        for name in ("ignored.h", "ignored.hpp"):
            with self.subTest(name=name):
                graph = lib.make(files(f"SRCS({name})"), "consumer")
                consumer = lib.node_by_output(graph, "$(B)/consumer/consumer.a")
                peer = lib.node_by_output(graph, "$(B)/g/g.a")
                self.assertIn(peer["uid"], consumer["deps"])
                self.assertNotIn("$(B)/g/generated.h", consumer["inputs"])
                self.assertFalse(any("$(B)/g/generated.h" in node["outputs"] for node in graph["graph"]))

    def test_explicit_src_header_requires_a_source_processing_output(self):
        for copy_first in (False, True):
            with self.subTest(copy_first=copy_first):
                sources = files("SRC(ignored.h)")
                if copy_first:
                    sources["g/ya.make"] = sources["g/ya.make"].replace(
                        "SRC(ignored.h)\nCOPY_FILE(seed.txt generated.h)",
                        "COPY_FILE(seed.txt generated.h)\nSRC(ignored.h)")
                result = lib.make_process(sources, "consumer")
                self.assertEqual(result.returncode, 1)
                self.assertIn("SRC", result.stderr)
                self.assertIn("no outputs", result.stderr)

    def test_cpp_peer_keeps_its_exported_header_inputs(self):
        sources = files("SRCS(ignored.h)")
        sources["g/ya.make"] = sources["g/ya.make"].replace("GO_LIBRARY()", "LIBRARY()").replace("SRCS(g.go)", "SRCS(g.cpp)")
        sources["g/g.cpp"] = "int native_value;\n"
        graph = lib.make(sources, "consumer")
        consumer = lib.node_by_output(graph, "$(B)/consumer/consumer.a")
        producer = lib.node_by_output(graph, "$(B)/g/generated.h")
        native = lib.node_by_output(graph, "$(B)/g/libg.a")
        self.assertIn("$(B)/g/generated.h", consumer["inputs"])
        self.assertIn(producer["uid"], consumer["deps"])
        self.assertIn(native["uid"], consumer["deps"])

if __name__ == "__main__":
    unittest.main(verbosity=2)
