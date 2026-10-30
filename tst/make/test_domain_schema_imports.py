from tst.make.lib import MakeTest, PROJECT


class DomainSchemaImportsTest(MakeTest):
    def setUp(self):
        super().setUp()
        self.write("ya.make", f"LIBRARY()\nADDINCL(FOR sc {PROJECT})\nSRCS(answer.sc)\nRESOURCE(answer.sc.h /schema)\nEND()\n")

    def test_include_whitespace_and_trailing_tokens(self):
        names = []
        lines = []
        for index, whitespace in enumerate(("\t", "\v", "\f", "\r", " \t\v\f\r ")):
            name = f"dependency{index}.sc"
            names.append(name)
            self.write(name, "\n")
            lines.append(f'\tinclude{whitespace}"{name}"{whitespace}; trailing tokens')
        self.write("answer.sc", "\n".join(lines) + "\n")
        inputs = self.node(self.graph(), "answer.sc.h")["inputs"]
        for name in names:
            self.assertIn(f"$(S)/{PROJECT}/{name}", inputs)

    def test_truncated_and_malformed_includes_are_ignored(self):
        statement = 'include \t"ignored.sc"\t;'
        fragments = [statement[:end] for end in range(len(statement))]
        fragments.extend(('include"ignored.sc";', "include 'ignored.sc';", 'include "";',
                          'include "ignored.sc" unexpected;', 'includX "ignored.sc";',
                          'incluXe "ignored.sc";', 'include \t? "ignored.sc";'))
        self.write("answer.sc", "\n".join(fragments) + '\ninclude "dependency.sc";\n')
        self.write("dependency.sc", "\n")
        self.write("ignored.sc", "\n")
        inputs = self.node(self.graph(), "answer.sc.h")["inputs"]
        self.assertIn(f"$(S)/{PROJECT}/dependency.sc", inputs)
        self.assertNotIn(f"$(S)/{PROJECT}/ignored.sc", inputs)

    def test_comments_do_not_create_include_dependencies(self):
        self.write("answer.sc", '// include "ignored.sc";\n/* include "ignored.sc"; */\ninclude "dependency.sc";\n')
        self.write("dependency.sc", "\n")
        self.write("ignored.sc", "\n")
        inputs = self.node(self.graph(), "answer.sc.h")["inputs"]
        self.assertIn(f"$(S)/{PROJECT}/dependency.sc", inputs)
        self.assertNotIn(f"$(S)/{PROJECT}/ignored.sc", inputs)
