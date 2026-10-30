from tst.make.lib import MakeTest, PROJECT


class GeneratedSsqlsTest(MakeTest):
    def test_generated_database_schemas_keep_their_producer(self):
        self.write("ya.make", "LIBRARY()\nPEERDIR(metrika/core/libs/mysqlxx metrika/core/libs/sqxxl)\nCOPY_FILE(template.ssqls generated.ssqls)\nSRCS(generated.ssqls)\nEND()\n")
        self.write("template.ssqls", '<DbObject><namespace>e2e</namespace><name>Answer</name><only_sqxxl>true</only_sqxxl><fields><field type="Int8" name="Value"/></fields></DbObject>\n')
        graph = self.graph()
        copied = self.node(graph, "generated.ssqls")
        generated = self.node(graph, "generated.cpp")
        self.assertIn(f"$(B)/{PROJECT}/generated.ssqls", generated["inputs"])
        self.assertIn(copied["uid"], generated["deps"])
        compiled = self.node(graph, "generated.cpp.o")
        self.assertIn(generated["uid"], compiled["deps"])
