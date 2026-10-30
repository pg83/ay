from tst.make.lib import MakeTest


class UberjarRuntimeOutputsTest(MakeTest):
    def test_uberjar_runtime_bundles_keep_scripts_and_the_requested_jdk(self):
        self.write("ya.make", "JAVA_PROGRAM(answer)\nJDK_VERSION(17)\nJAVA_SRCS(Main.java)\nUBERJAR()\nWITH_JDK()\nGENERATE_SCRIPT(OUT launch.sh TEMPLATE launch.tmpl)\nEND()\n")
        self.write("Main.java", "public class Main { public static void main(String[] args) {} }\n")
        self.write("launch.tmpl", "#!/bin/sh\njava -jar answer.jar\n")
        graph = self.graph()
        bundled = self.node(graph, "answer.tar")
        self.assertIn("$(B)/__ay_e2e__/launch.sh", bundled["outputs"])
        self.assertIn("$(B)/__ay_e2e__/jdk.tar", bundled["outputs"])
