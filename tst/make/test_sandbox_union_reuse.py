from tst.make.lib import MakeTest, PROJECT


class SandboxUnionReuseTest(MakeTest):
    def test_keep_going_keeps_healthy_roots_after_union_sandbox_output_collisions(self):
        self.write("ya.make", f"LIBRARY(answer)\nRUN_PROGRAM({PROJECT}/java_tool STDOUT from_java.cpp)\nBASE_CODEGEN({PROJECT}/install seed)\nSRCS(seed.cpp)\nEND()\nRECURSE(healthy)\n")
        self.write("seed.in", "42\n")
        self.write("java_tool/ya.make", f"JAVA_PROGRAM(java_generator)\nJAVA_SRCS(Main.java)\nPEERDIR({PROJECT}/first {PROJECT}/second)\nEND()\n")
        self.write("java_tool/Main.java", 'public class Main { public static void main(String[] args) { System.out.println("int java_answer() { return 17; }"); } }\n')
        self.write("first/ya.make", f"UNION(first)\nPEERDIR({PROJECT}/install)\nEND()\n")
        self.write("second/ya.make", f"LIBRARY(second)\nSET(PEERDIR_TAGS SDC_INSTALL_SHARE)\nPEERDIR({PROJECT}/install)\nSRCS(second.cpp)\nEND()\n")
        self.write("second/second.cpp", "int second() { return 2; }\n")
        self.write("install/ya.make", "SDC_UNION(install)\nFROM_SANDBOX(123456789 OUT_NOAUTO install.share.pkg.fake install.pkg.fake)\nEND()\n")
        self.write("healthy/ya.make", "PROGRAM(answer)\nSRCS(main.cpp)\nEND()\n")
        self.write("healthy/main.cpp", "int main() { return 0; }\n")
        graph = self.graph("-k")
        self.assertIn(self.node(graph, "healthy/answer")["uid"], graph["result"])
