import io
import tarfile

from tst.make.lib import MakeTest, PROJECT


class ArchivedEventSourcesTest(MakeTest):
    def test_archived_events_keep_declared_proto_dependencies(self):
        self.write("ya.make", f"LIBRARY()\nADDINCL({PROJECT})\nSRCS(dependency.proto)\nFROM_ARCHIVE(sources.tar OUT answer.ev OUTPUT_INCLUDES ${{ARCADIA_ROOT}}/{PROJECT}/dependency.proto)\nEND()\n")
        self.write("dependency.proto", 'syntax = "proto2";\nmessage Dependency {}\n')
        value = b'syntax = "proto2";\nimport "dependency.proto";\nmessage Answer { optional Dependency value = 1; }\n'
        with tarfile.open(self.project / "sources.tar", "w") as archive:
            entry = tarfile.TarInfo("answer.ev")
            entry.size = len(value)
            archive.addfile(entry, io.BytesIO(value))
        graph = self.graph()
        extracted = self.node(graph, "answer.ev")
        generated = self.node(graph, "answer.ev.pb.cc")
        self.assertIn(f"$(B)/{PROJECT}/answer.ev", generated["inputs"])
        self.assertIn(f"$(S)/{PROJECT}/dependency.proto", generated["inputs"])
        self.assertIn(extracted["uid"], generated["deps"])
        compiled = self.node(graph, "answer.ev.pb.cc.o")
        self.assertIn(f"$(B)/{PROJECT}/dependency.pb.h", compiled["inputs"])
        self.assertIn(self.node(graph, "dependency.pb.h")["uid"], compiled["deps"])
