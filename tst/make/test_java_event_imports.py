from tst.make.lib import MakeTest, PROJECT


class JavaEventImportsTest(MakeTest):
    def test_event_translation_keeps_imported_proto_sources(self):
        self.write("ya.make", f"JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nPEERDIR({PROJECT}/schema)\nEND()\n")
        self.write("schema/ya.make", "PROTO_LIBRARY()\nSRCS(answer.ev)\nEND()\n")
        self.write("schema/answer.ev", f'syntax = "proto2";\nimport "{PROJECT}/schema/base.proto";\nmessage Answer {{ optional Base value = 1; }}\n')
        self.write("schema/base.proto", 'syntax = "proto2";\nmessage Base { optional int32 value = 1; }\n')
        node = self.node(self.graph(), "schema/answer_ev.proto")
        self.assertIn(f"$(S)/{PROJECT}/schema/base.proto", node["inputs"])
