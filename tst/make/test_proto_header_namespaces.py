from tst.make.lib import MakeTest, PROJECT


class ProtoHeaderNamespacesTest(MakeTest):
    def test_config_and_event_imports_use_the_declared_source_namespace(self):
        self.write("ya.make", f"LIBRARY()\nPROTO_NAMESPACE({PROJECT})\nSRCS(answer.cfgproto event.ev base.proto)\nEND()\n")
        self.write("base.proto", 'syntax = "proto2";\nmessage Base {}\n')
        self.write("answer.cfgproto", 'syntax = "proto2";\nimport "base.proto";\nmessage Answer {}\n')
        self.write("event.ev", 'syntax = "proto2";\nimport "nested/../base.proto";\nmessage Event {}\n')
        self.write("nested/.keep", "")
        graph = self.graph()
        for output in ("answer.cfgproto.pb.cc.o", "event.ev.pb.cc.o"):
            with self.subTest(output=output):
                self.assertIn(f"$(B)/{PROJECT}/base.pb.h", self.node(graph, output)["inputs"])

    def test_well_known_imports_map_to_the_protobuf_runtime_headers(self):
        self.write("ya.make", "LIBRARY()\nSRCS(answer.cfgproto)\nEND()\n")
        self.write("answer.cfgproto", 'syntax = "proto2";\nimport "google/protobuf/descriptor.proto";\nmessage Answer {}\n')
        self.assertIn("$(S)/contrib/libs/protobuf/src/google/protobuf/descriptor.pb.h", self.node(self.graph(), "answer.cfgproto.pb.cc.o")["inputs"])
