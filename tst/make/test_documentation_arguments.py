from tst.make.lib import MakeTest, PROJECT


class DocumentationArgumentsTest(MakeTest):
    def test_copy_keywords_require_values(self):
        for keyword in ("FROM", "NAMESPACE"):
            with self.subTest(keyword=keyword):
                self.write("ya.make", f"DOCS()\nDOCS_COPY_FILES(index.md {keyword})\nEND()\n")
                self.write("index.md", "# Answer\n")
                self.assertNotEqual(self.make().returncode, 0)

    def test_source_directory_requires_a_value(self):
        self.write("ya.make", "DOCS_LIBRARY(answer)\nSRCS(SRCDIR)\nEND()\n")
        self.assertNotEqual(self.make().returncode, 0)

    def test_variables_require_an_assignment(self):
        self.write("ya.make", "DOCS()\nDOCS_VARS(answer)\nEND()\n")
        self.write(".yfm", "title: Answer\n")
        self.write("index.md", "# Answer\n")
        self.assertNotEqual(self.make().returncode, 0)

    def test_flat_exclusion_patterns_match_only_the_named_source(self):
        self.write("ya.make", "DOCS_LIBRARY(answer)\nSRCS(*.md EXCLUDE ignored.md)\nEND()\n")
        self.write("index.md", "# Answer\n")
        self.write("ignored.md", "# Ignored\n")
        inputs = self.node(self.graph(), "answer.docslib")["inputs"]
        self.assertIn(f"$(S)/{PROJECT}/index.md", inputs)
        self.assertNotIn(f"$(S)/{PROJECT}/ignored.md", inputs)
