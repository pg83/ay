from tst.make.lib import MakeTest


class JavaLateVariableFragmentsTest(MakeTest):
    def test_java_generators_reject_incomplete_late_variable_references(self):
        self.write("tool/ya.make", "JAVA_PROGRAM(generator)\nJDK_VERSION(17)\nJAVA_SRCS(Generator.java)\nEND()\n")
        self.write("tool/Generator.java", "public class Generator { public static void main(String[] args) {} }\n")
        self.write("ya.make", f"JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nRUN_JAVA_PROGRAM(Generator $E2E_FRAGMENT CLASSPATH {self.project_name}/tool OUT Generated.java)\nJAVA_SRCS(Generated.java)\nEND()\n")
        for fragment in ("${", "${{", "${$"):
            with self.subTest(fragment=fragment):
                self.assertNotEqual(self.make("-DE2E_FRAGMENT=" + fragment).returncode, 0)
