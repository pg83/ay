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
