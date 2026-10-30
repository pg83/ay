from tst.make.lib import MakeTest


class DocumentationPeerValidationTest(MakeTest):
    def test_native_libraries_cannot_supply_documentation_peer_variants(self):
        self.write("ya.make", f"DOCS_LIBRARY(answer)\nPEERDIR({self.project_name}/native)\nSRCS(index.md)\nEND()\n")
        self.write("index.md", "# Answer\n")
        self.write("native/ya.make", "LIBRARY()\nSRCS(answer.cpp)\nEND()\n")
        self.write("native/answer.cpp", "int answer() { return 42; }\n")
        self.assertNotEqual(self.make().returncode, 0)
