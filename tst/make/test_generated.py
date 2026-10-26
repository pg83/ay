from tst.make.lib import MakeTest, PROJECT


class GeneratedSourceTest(MakeTest):
    def test_copy_file_producer_and_consumer(self):
        self.write("ya.make", "LIBRARY()\nCOPY_FILE(template.cpp generated.cpp)\nSRCS(${BINDIR}/generated.cpp)\nEND()\n")
        self.write("template.cpp", "int answer() { return 42; }\n")
        graph = self.graph()
        producer = self.node(graph, "generated.cpp")
        consumer = self.node(graph, "generated.cpp.o")
        self.assertIn(f"$(S)/{PROJECT}/template.cpp", producer["inputs"])
        self.assertIn(f"$(B)/{PROJECT}/generated.cpp", consumer["inputs"])
        self.assertIn(producer["uid"], consumer["deps"])

    def test_flatbuffers_schema_from_copy_file(self):
        self.write("ya.make", "FBS_LIBRARY()\nCOPY_FILE(schema/answer.fbs answer.fbs)\nSRCS(answer.fbs)\nEND()\n")
        self.write("schema/answer.fbs", "namespace e2e; table Answer { value:int; } root_type Answer;\n")
        graph = self.graph()
        copied = self.node(graph, "answer.fbs")
        header = self.node(graph, "answer.fbs.h")
        self.assertIn(f"$(S)/{PROJECT}/schema/answer.fbs", copied["inputs"])
        self.assertIn(f"$(B)/{PROJECT}/answer.fbs", header["inputs"])
        self.assertIn(copied["uid"], header["deps"])
        consumers = [node for node in graph["graph"] if f"$(B)/{PROJECT}/answer.fbs" in node["inputs"]]
        self.assertGreater(len(consumers), 1)
        self.assertFalse([node["outputs"] for node in graph["graph"] if f"$(S)/{PROJECT}/answer.fbs" in node["inputs"]])
