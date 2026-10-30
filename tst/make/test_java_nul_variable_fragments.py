from tst.make.lib import MakeTest


class JavaNulVariableFragmentsTest(MakeTest):
    def test_java_generators_reject_nul_in_a_configured_variable_reference(self):
        core = self.root / "build/ymake.core.conf"
        self.write_tree("build/ymake.core.conf", core.read_text() + "\nE2E_NUL_REFERENCE=${\x00}\n")
        self.write("tool/ya.make", "JAVA_PROGRAM(generator)\nJDK_VERSION(17)\nJAVA_SRCS(Generator.java)\nEND()\n")
        self.write("tool/Generator.java", "public class Generator { public static void main(String[] args) {} }\n")
        self.write("ya.make", f"JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nRUN_JAVA_PROGRAM(Generator $E2E_NUL_REFERENCE CLASSPATH {self.project_name}/tool OUT Generated.java)\nJAVA_SRCS(Generated.java)\nEND()\n")
        self.assertNotEqual(self.make().returncode, 0)
