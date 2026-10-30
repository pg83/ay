from tst.make.lib import MakeTest, PROJECT


class ProtoSourcePathsTest(MakeTest):
    def test_parent_segments_resolve_in_source_directories(self):
        for extension in ("ev", "cfgproto"):
            for directory in ("", "src"):
                with self.subTest(extension=extension, directory=directory):
                    self.write("ya.make", f"LIBRARY()\nSRCDIR({PROJECT}/empty {PROJECT}/src)\nSRCS(nested/../answer.{extension})\nEND()\n")
                    for path in ("empty/.keep", "nested/.keep", "src/nested/.keep"):
                        self.write(path, "")
                    source = f"{directory}/answer.{extension}".lstrip("/")
                    self.write(source, 'syntax = "proto2";\npackage e2e;\nmessage Answer { optional int32 value = 1; }\n')
                    graph = self.graph()
                    protoc = [node for node in graph["graph"] if f"$(S)/{PROJECT}/{source}" in node["inputs"] and any(path.endswith(".pb.cc") for path in node["outputs"])]
                    self.assertEqual(len(protoc), 1)
                    (self.project / source).unlink()

    def test_missing_proto_with_parent_segments_is_an_error(self):
        self.write("ya.make", f"LIBRARY()\nSRCDIR({PROJECT}/empty)\nSRCS(nested/../missing.ev)\nEND()\n")
        self.write("empty/nested/.keep", "")
        self.write("nested/.keep", "")
        self.assertNotEqual(self.make().returncode, 0)
