from tst.make.lib import MakeTest


class UnversionedToolchainMetadataTest(MakeTest):
    def test_toolchain_metadata_uses_unknown_for_an_unspecified_version(self):
        self.write("ya.make", f"JAVA_PROGRAM(answer)\nJDK_VERSION(17)\nENABLE(EMBED_SBOM)\nPEERDIR({self.project_name}/toolchain)\nJAVA_SRCS(Main.java)\nEND()\n")
        self.write("Main.java", "public class Main { public static void main(String[] args) {} }\n")
        self.write("toolchain/ya.make", "RESOURCES_LIBRARY()\nTOOLCHAIN(E2E_TOOLCHAIN)\nDECLARE_EXTERNAL_RESOURCE(E2E_CUSTOM sbr:123456789)\nEND()\n")
        component = self.node(self.graph(), "toolchain/toolchain.component.sbom")
        arguments = [arg for command in component["cmds"] for arg in command["cmd_args"]]
        self.assertEqual(arguments[arguments.index("--ver") + 1], "unknown")
        self.assertEqual(arguments[arguments.index("--toolchain-name") + 1], "E2E_TOOLCHAIN")
