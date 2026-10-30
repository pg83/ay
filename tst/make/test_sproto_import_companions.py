from tst.make.lib import MakeTest, PROJECT


class SprotoImportCompanionsTest(MakeTest):
    def test_imported_schemas_keep_their_sproto_header_companions(self):
        self.write("ya.make", "LIBRARY()\nSRCS(base.proto answer.proto)\nYMAPS_SPROTO(base.proto answer.proto)\nRESOURCE(answer.sproto.h /schema)\nEND()\n")
        self.write("base.proto", 'syntax = "proto2";\npackage e2e;\nmessage Base { optional int32 value = 1; }\n')
        self.write("answer.proto", f'syntax = "proto2";\npackage e2e;\nimport "{PROJECT}/base.proto";\nmessage Answer {{ optional Base value = 1; }}\n')
        graph = self.graph()
        imported = self.node(graph, "base.sproto.h")
        compiled = self.node(graph, "answer.pb.cc.o")
        self.assertIn(f"$(B)/{PROJECT}/base.sproto.h", compiled["inputs"])
        self.assertIn(imported["uid"], compiled["deps"])
