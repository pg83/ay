from tst.make.lib import MakeTest, PROJECT


class NativeConfigTest(MakeTest):
    def test_global_c_flags_reach_linker(self):
        self.write("ya.make", f"PROGRAM(answer)\nSRCS(main.cpp)\nPEERDIR({PROJECT}/lib)\nEND()\n")
        self.write("main.cpp", "int main() { return 0; }\n")
        self.write("lib/ya.make", "LIBRARY()\nCONLYFLAGS(GLOBAL -DE2E_FIRST GLOBAL -DE2E_SECOND)\nSRCS(value.c)\nEND()\n")
        self.write("lib/value.c", "int value(void) { return 42; }\n")
        node = self.node(self.graph(), "answer")
        args = [arg for command in node["cmds"] for arg in command["cmd_args"]]
        for flag in ("-DE2E_FIRST", "-DE2E_SECOND"):
            self.assertIn(flag, args)

    def test_windows_manifest(self):
        self.write("main.cpp", "int main() { return 0; }\n")
        for macros in ("WINDOWS_LONG_PATH_MANIFEST()", "WINDOWS_MANIFEST(build/long-path.manifest)"):
            with self.subTest(macros=macros):
                self.write("ya.make", f"PROGRAM(answer)\n{macros}\nSRCS(main.cpp)\nEND()\n")
                self.graph("--target-platform=default-win-x86_64")

    def test_compiler_features(self):
        self.write("answer.cpp", "int answer() { return 42; }\n")
        for macros in (
            "NO_COW()\nNO_NEED_CHECK()\nNO_COMPILER_WARNINGS()",
            "ENABLE(UBSAN)\nNO_SANITIZE()",
            "CXXFLAGS(GLOBAL -DE2E=42)\nCFLAGS(GLOBAL -DE2E_GLOBAL=1)",
            "NO_WSHADOW()\nNO_OPTIMIZE()",
        ):
            with self.subTest(macros=macros):
                self.write("ya.make", f"LIBRARY()\n{macros}\nSRCS(answer.cpp)\nEND()\n")
                self.node(self.graph(), "answer.cpp.o")

    def test_module_reuse_and_tag_selection(self):
        self.write("ya.make", f"PROGRAM(answer)\nSRCS(main.cpp)\nPEERDIR({PROJECT}/left {PROJECT}/right)\nEND()\n")
        self.write("main.cpp", "int main() { return 0; }\n")
        for name in ("left", "right"):
            self.write(name + "/ya.make", f"LIBRARY()\nPEERDIR({PROJECT}/shared)\nSRCS({name}.cpp)\nEND()\n")
            self.write(name + "/" + name + ".cpp", f"int {name}() {{ return 42; }}\n")
        self.write("shared/ya.make", "LIBRARY()\nSRCS(shared.cpp)\nEND()\n")
        self.write("shared/shared.cpp", "int shared() { return 42; }\n")
        self.node(self.graph(), "shared/shared.cpp.o")
