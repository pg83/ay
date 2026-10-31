from tst.make.lib import MakeTest, PROJECT


class SandboxProgramReuseTest(MakeTest):
    def test_reused_program_tools_reject_sandbox_outputs_that_match_the_binary_name(self):
        self.write("ya.make", f"LIBRARY(answer)\nRUN_PROGRAM({PROJECT}/java_tool STDOUT from_java.cpp)\nRUN_PROGRAM({PROJECT}/native_tool STDOUT from_native.cpp)\nEND()\n")
        self.write("java_tool/ya.make", f"JAVA_PROGRAM(java_generator)\nJAVA_SRCS(Main.java)\nPEERDIR({PROJECT}/native_tool)\nEND()\n")
        self.write("java_tool/Main.java", 'public class Main { public static void main(String[] args) { System.out.println("int java_answer() { return 17; }"); } }\n')
        self.write("native_tool/ya.make", "PROGRAM(generator)\nFROM_SANDBOX(FILE 123456789 OUT_NOAUTO generator)\nSRCS(main.cpp GLOBAL extra.cpp)\nEND()\n")
        self.write("native_tool/main.cpp", '#include <cstdio>\nint main() { std::puts("int native_answer() { return 42; }"); }\n')
        self.write("native_tool/extra.cpp", "int extra() { return 17; }\n")
        result = self.make()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("generator", result.stderr)
