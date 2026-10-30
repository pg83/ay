from tst.make.lib import MakeTest, PROJECT


class YqlArchiveSuffixesTest(MakeTest):
    def test_embedded_archives_keep_distinct_abi_output_names(self):
        self.write("ya.make", f"PROGRAM(consumer)\nYQL_CURRENT_ABI_VERSION()\nSRCS(main.cpp)\nPEERDIR({PROJECT}/variant)\nEND()\nRECURSE(variant)\n")
        self.write("main.cpp", "int main() { return 0; }\n")
        self.write("variant/ya.make", "YQL_LIBRARY(answer)\nARCHIVE_ASM(NAME data DONTCOMPRESS value.txt)\nEND()\n")
        self.write("variant/value.txt", "42\n")
        graph = self.graph()
        for abi in ("stable", "current"):
            with self.subTest(abi=abi):
                node = self.node(graph, f"variant/data.abi_{abi}.rodata")
                self.assertIn(f"$(S)/{PROJECT}/variant/value.txt", node["inputs"])
