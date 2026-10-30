from tst.make.lib import MakeTest, PROJECT


class NestedJavaManagementTest(MakeTest):
    def test_management_paths_outside_contrib_are_rejected(self):
        self.write("ya.make", f"JAVA_PROGRAM(answer)\nJDK_VERSION(17)\nJAVA_SRCS(Main.java)\nPEERDIR({PROJECT}/lib/nested)\nDEPENDENCY_MANAGEMENT({PROJECT}/lib/nested/deep {PROJECT}/lib/nested)\nEND()\n")
        self.write("Main.java", "public class Main {}\n")
        for directory in ("lib/nested", "lib/nested/deep"):
            self.write(directory + "/ya.make", "JAVA_LIBRARY(value)\nJDK_VERSION(17)\nJAVA_SRCS(Value.java)\nEND()\n")
            self.write(directory + "/Value.java", "public class Value {}\n")
        self.assertNotEqual(self.make().returncode, 0)
