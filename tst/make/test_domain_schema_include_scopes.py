from tst.make.lib import MakeTest, PROJECT


class DomainSchemaIncludeScopesTest(MakeTest):
    def test_one_level_schema_include_directories_are_exported_to_the_consumer(self):
        self.write("ya.make", f"LIBRARY(answer)\nPEERDIR({PROJECT}/peer)\nSRCS(answer.sc)\nRESOURCE(answer.sc.h /schema)\nEND()\n")
        self.write("answer.sc", 'include "base.sc";\n')
        self.write("peer/ya.make", f"LIBRARY(peer)\nADDINCL(ONE_LEVEL FOR sc {PROJECT}/defs)\nEND()\n")
        self.write("defs/base.sc", "\n")
        generated = self.node(self.graph(), "answer.sc.h")
        self.assertIn(f"$(S)/{PROJECT}/defs/base.sc", generated["inputs"])
