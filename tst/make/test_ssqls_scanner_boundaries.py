from tst.make.lib import MakeTest, PROJECT


class SsqlsScannerBoundariesTest(MakeTest):
    document = '<DbObject><namespace>e2e</namespace><name>Answer</name><only_sqxxl>true</only_sqxxl>{includes}<fields><field type="Int8" name="Value"/></fields></DbObject>\n'

    def fixture(self):
        self.write("ya.make", f"LIBRARY()\nADDINCL({PROJECT})\nPEERDIR(metrika/core/libs/mysqlxx metrika/core/libs/sqxxl)\nSRCS(answer.ssqls)\nEND()\n")

    def test_include_path_attributes_keep_xml_dependencies(self):
        self.fixture()
        self.write("answer.ssqls", self.document.format(includes='<include path="base.ssqls"/>'))
        self.write("base.ssqls", self.document.format(includes=""))
        generated = self.node(self.graph(), "answer.h")
        self.assertIn(f"$(S)/{PROJECT}/base.ssqls", generated["inputs"])

    def test_rooted_include_paths_keep_source_and_generated_xml_dependencies(self):
        self.fixture()
        self.write("ya.make", (self.project / "ya.make").read_text().replace("SRCS(answer.ssqls)", "COPY_FILE(template.ssqls generated.ssqls)\nSRCS(answer.ssqls)"))
        self.write("answer.ssqls", self.document.format(includes=f'<include path="$S/{PROJECT}/base.ssqls"/><include path="$B/{PROJECT}/generated.ssqls"/>'))
        for name in ("base.ssqls", "template.ssqls"):
            self.write(name, self.document.format(includes=""))
        graph = self.graph()
        generated = self.node(graph, "answer.h")
        for path in (f"$(S)/{PROJECT}/base.ssqls", f"$(B)/{PROJECT}/generated.ssqls"):
            self.assertIn(path, generated["inputs"])
        self.assertIn(self.node(graph, "generated.ssqls")["uid"], generated["deps"])

    def test_unmatched_closing_elements_are_configuration_errors(self):
        self.fixture()
        for text in ("</DbObject>", "<DbObject></Other>"):
            with self.subTest(text=text):
                self.write("answer.ssqls", text)
                self.assertNotEqual(self.make().returncode, 0)

    def test_empty_and_incomplete_xml_documents_are_configuration_errors(self):
        self.fixture()
        for text in ("", "<!-- no document -->", "<DbObject>"):
            with self.subTest(text=text):
                self.write("answer.ssqls", text)
                self.assertNotEqual(self.make().returncode, 0)
