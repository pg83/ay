import unittest

import lib
from test_go_tests import go_world


class GoNativeSourcesTest(unittest.TestCase):
    def test_native_sources_bind_outputs_from_the_declared_argument(self):
        for extension in ("c", "cpp", "S"):
            for relative in (f"bridge.{extension}", f"nested/bridge.{extension}"):
                with self.subTest(extension=extension, relative=relative):
                    files = go_world()
                    files.update({
                        "g/ya.make": f"GO_LIBRARY()\n{lib.BARE_MODULE}SRCDIR(shared)\nSRCS(g.go {relative})\nEND()\n",
                        "g/g.go": "package g\n",
                        "shared/" + relative: "\n",
                    })
                    graph = lib.make(files, "g")
                    output = "$(B)/g/" + relative + ".o"
                    native = lib.node_by_output(graph, output)
                    go = lib.node_by_output(graph, "$(B)/g/g.a")
                    self.assertIn("$(S)/shared/" + relative, native["inputs"])
                    self.assertIn(output, go["inputs"])
                    self.assertIn(native["uid"], go["deps"])

    def test_explicit_source_and_build_roots_keep_their_output_roots(self):
        for source, output in (("${ARCADIA_ROOT}/shared/bridge.cpp", "$(B)/shared/bridge.cpp.o"), ("${BINDIR}/generated.cpp", "$(B)/g/generated.cpp.o")):
            with self.subTest(source=source):
                files = go_world()
                files.update({
                    "g/ya.make": f"GO_LIBRARY()\n{lib.BARE_MODULE}COPY_FILE(seed.txt generated.cpp)\nSRCS(g.go {source})\nEND()\n",
                    "g/g.go": "package g\n",
                    "g/seed.txt": "\n",
                    "shared/bridge.cpp": "\n",
                })
                graph = lib.make(files, "g")
                native = lib.node_by_output(graph, output)
                go = lib.node_by_output(graph, "$(B)/g/g.a")
                self.assertIn(output, go["inputs"])
                self.assertIn(native["uid"], go["deps"])
                if "BINDIR" in source:
                    producer = lib.node_by_output(graph, "$(B)/g/generated.cpp")
                    self.assertIn("$(B)/g/generated.cpp", native["inputs"])
                    self.assertIn(producer["uid"], native["deps"])
                else:
                    self.assertIn("$(S)/shared/bridge.cpp", native["inputs"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
