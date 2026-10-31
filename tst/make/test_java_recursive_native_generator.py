from tst.make.lib import MakeTest, PROJECT


class JavaRecursiveNativeGeneratorTest(MakeTest):
    def test_java_program_cannot_use_a_native_generator_that_requires_its_own_output(self):
        for module in ("LIBRARY", "DLL"):
            with self.subTest(module=module):
                recurse = "RECURSE(app)\n" if module == "DLL" else ""
                self.write("ya.make", f"LIBRARY(answer)\nBASE_CODEGEN({PROJECT}/java_tool answer)\nSRCS(answer.cpp)\nEND()\n{recurse}")
                self.write("answer.in", "42\n")
                self.write("java_tool/ya.make", f"JAVA_PROGRAM(generator)\nJDK_VERSION(17)\nJAVA_SRCS(Main.java)\nPEERDIR({PROJECT}/native)\nEND()\n")
                self.write("java_tool/Main.java", 'public class Main { public static void main(String[] args) { System.out.println("int answer() { return 42; }"); } }\n')
                self.write("native/ya.make", f"{module}(native)\nBASE_CODEGEN({PROJECT}/java_tool seed)\nSRCS(seed.cpp)\nEND()\n")
                self.write("native/seed.in", "17\n")
                if module == "DLL":
                    self.write("app/ya.make", f"PROGRAM(app)\nPEERDIR({PROJECT}/native)\nSRCS(main.cpp)\nEND()\n")
                    self.write("app/main.cpp", "int main() { return 0; }\n")
                self.assertNotEqual(self.make().returncode, 0)

    def test_java_program_cannot_use_a_go_generator_that_requires_its_own_output(self):
        self.write("ya.make", f"LIBRARY(answer)\nBASE_CODEGEN({PROJECT}/java_tool first)\nBASE_CODEGEN({PROJECT}/go_tool second)\nSRCS(first.cpp second.cpp)\nEND()\n")
        self.write("first.in", "42\n")
        self.write("second.in", "17\n")
        self.write("java_tool/ya.make", f"JAVA_PROGRAM(generator)\nJDK_VERSION(17)\nJAVA_SRCS(Main.java)\nRUN_PROGRAM({PROJECT}/go_tool STDOUT Generated.java)\nEND()\n")
        self.write("java_tool/Main.java", 'public class Main { public static void main(String[] args) { System.out.println("int answer() { return 42; }"); } }\n')
        self.write("go_tool/ya.make", f"GO_PROGRAM(generator)\nGO_PROTO_PLUGIN(e2e .e2e.go {PROJECT}/java_tool)\nSRCS(main.go answer.proto)\nEND()\n")
        self.write("go_tool/main.go", "package main\nfunc main() {}\n")
        self.write("go_tool/answer.proto", f'syntax = "proto3"; option go_package = "a.yandex-team.ru/{PROJECT}/go_tool;main"; message Answer {{ int32 value = 1; }}\n')
        self.assertNotEqual(self.make().returncode, 0)
