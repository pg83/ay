from tst.make.lib import MakeTest, PROJECT


class DocumentationContextsTest(MakeTest):
    def test_explicit_sources_and_copy(self):
        self.write("ya.make", f"DOCS(answer)\nDOCS_CONFIG(config.yml)\nDOCS_DIR({PROJECT}/pages)\nDOCS_INCLUDE_SOURCES({PROJECT}/extra.txt)\nDOCS_COPY_FILES(value.txt FROM ${{ARCADIA_ROOT}}/{PROJECT}/assets NAMESPACE images)\nDOCS_VARS(title=Ответ emoji=🙂 html=<tag>)\nUSE_PLANTUML()\nEND()\n")
        self.write("config.yml", "title: Answer\n")
        self.write("pages/toc.yaml", "title: Answer\nitems:\n  - name: Answer\n    href: index.md\n")
        self.write("pages/index.md", "# {{ title }}\n")
        self.write("assets/value.txt", "42\n")
        self.write("extra.txt", "extra\n")
        graph = self.graph()
        self.assertIn(f"$(S)/{PROJECT}/config.yml", self.node(graph, "answer.tar.gz")["inputs"])
        self.assertTrue(any(f"$(S)/{PROJECT}/assets/value.txt" in node["inputs"] for node in graph["graph"]))

    def test_library_explicit_source_root(self):
        self.write("ya.make", "DOCS_LIBRARY(answer)\nSRCS(SRCDIR pages **/a?.md EXCLUDE **/ignored/**)\nEND()\n")
        self.write("pages/nested/a1.md", "# Answer\n")
        self.write("pages/ignored/a2.md", "# Ignored\n")
        inputs = self.node(self.graph(), "answer.docslib")["inputs"]
        self.assertIn(f"$(S)/{PROJECT}/pages/nested/a1.md", inputs)
        self.assertNotIn(f"$(S)/{PROJECT}/pages/ignored/a2.md", inputs)
