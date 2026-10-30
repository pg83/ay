import io
import tarfile

from tst.make.lib import MakeTest, PROJECT


class ArchivedPythonProtoTest(MakeTest):
    def check_archived_source(self, extension):
        source = "answer" + extension
        self.write("ya.make", f"PROTO_LIBRARY()\nONLY_TAGS(PY3_PROTO)\nFROM_ARCHIVE(sources.tar OUT {source})\nEND()\n")
        value = b'syntax = "proto2";\nmessage Answer {}\n'
        with tarfile.open(self.project / "sources.tar", "w") as archive:
            entry = tarfile.TarInfo(source)
            entry.size = len(value)
            archive.addfile(entry, io.BytesIO(value))
        graph = self.graph()
        extracted = self.node(graph, source)
        output = "answer" + (".ev" if extension == ".ev" else "") + ".pb.cc"
        generated = self.node(graph, output)
        self.assertIn(f"$(B)/{PROJECT}/{source}", generated["inputs"])
        self.assertIn(extracted["uid"], generated["deps"])
        compiled = self.node(graph, output + ".py3.o")
        self.assertIn(f"$(B)/{PROJECT}/{output}", compiled["inputs"])
        self.assertIn(generated["uid"], compiled["deps"])

    def test_archived_protobuf_sources_keep_native_compilation_in_python_variants(self):
        self.check_archived_source(".proto")

    def test_archived_events_keep_native_compilation_in_python_variants(self):
        self.check_archived_source(".ev")
