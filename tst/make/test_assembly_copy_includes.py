from tst.make.lib import MakeTest


class AssemblyCopyIncludesTest(MakeTest):
    def test_copying_assembly_with_context_keeps_its_included_files(self):
        self.write("ya.make", "LIBRARY()\nCOPY_FILE_WITH_CONTEXT(answer.asm generated.asm)\nSRCS(generated.asm)\nEND()\n")
        self.write("answer.asm", '%include "constants.inc"\nsection .text\n')
        self.write("constants.inc", "%define ANSWER 42\n")
        copied = self.node(self.graph(), "generated.asm")
        self.assertIn(f"$(S)/{self.project_name}/constants.inc", copied["inputs"])
