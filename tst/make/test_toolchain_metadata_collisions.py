from tst.make.lib import MakeTest


class ToolchainMetadataCollisionsTest(MakeTest):
    def test_toolchain_metadata_cannot_share_an_output_with_an_ordinary_copy(self):
        self.write("ya.make", f"JAVA_PROGRAM(answer)\nJDK_VERSION(17)\nENABLE(EMBED_SBOM)\nPEERDIR({self.project_name}/toolchain)\nJAVA_SRCS(Main.java)\nEND()\n")
        self.write("Main.java", "public class Main { public static void main(String[] args) {} }\n")
        self.write("toolchain/ya.make", "RESOURCES_LIBRARY()\nTOOLCHAIN(E2E_TOOLCHAIN)\nVERSION(1)\nCOPY_FILE(template.json toolchain.component.sbom)\nEND()\n")
        self.write("toolchain/template.json", "{}\n")
        self.assertNotEqual(self.make().returncode, 0)
