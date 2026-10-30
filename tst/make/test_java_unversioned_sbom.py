from tst.make.lib import MakeTest, PROJECT


class JavaUnversionedSbomTest(MakeTest):
    def test_explicitly_empty_contrib_version_stays_empty_in_sbom(self):
        self.write("ya.make", f"JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nPEERDIR({PROJECT}/contrib)\nEND()\n")
        self.write("contrib/ya.make", "JAVA_CONTRIB(dependency)\nSET(MODVER)\nLICENSE(MIT)\nLOCAL_JAR(dependency.jar)\nEND()\n")
        self.write("contrib/dependency.jar", "")
        node = self.node(self.graph("--build=release"), "contrib/dependency.component.sbom")
        args = next(command["cmd_args"] for command in node["cmds"] if any(arg.endswith("/gen_sbom.py") for arg in command["cmd_args"]))
        self.assertEqual(args[args.index("--ver") + 1:args.index("--lang")], [])
