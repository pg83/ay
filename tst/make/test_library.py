from tst.make.lib import MakeTest, PROJECT


class LibraryTest(MakeTest):
    def test_headers_update_between_invocations(self):
        self.write("ya.make", "LIBRARY()\nSRCS(a.cpp)\nEND()\n")
        self.write("a.cpp", '#include "a.h"\nint answer() { return VALUE; }\n')
        self.write("a.h", "#define VALUE 42\n")
        first = self.node(self.graph(), "a.cpp.o")
        self.assertIn(f"$(S)/{PROJECT}/a.h", first["inputs"])
        self.assertNotIn(f"$(S)/{PROJECT}/b.h", first["inputs"])

        self.write("a.h", '#include "b.h"\n')
        self.write("b.h", "#define VALUE 137\n")
        second = self.node(self.graph(), "a.cpp.o")
        self.assertIn(f"$(S)/{PROJECT}/a.h", second["inputs"])
        self.assertIn(f"$(S)/{PROJECT}/b.h", second["inputs"])

    def test_macro_peerdir_keeps_statement_position(self):
        self.write("ya.make", f"PROGRAM(answer)\nSRCS(main.cpp)\nPEERDIR({PROJECT}/lib)\nEND()\n")
        self.write("main.cpp", "int main() { return 0; }\n")
        self.write("lib/ya.make", f"LIBRARY()\nGENERATE_ENUM_SERIALIZATION(answer.h)\nSRCS(answer.cpp)\nPEERDIR({PROJECT}/dep)\nEND()\n")
        self.write("lib/answer.h", "#pragma once\nenum class EAnswer { Yes, No };\n")
        self.write("lib/answer.cpp", '#include "answer.h"\nint answer() { return 42; }\n')
        self.write("dep/ya.make", "LIBRARY()\nSRCS(dep.cpp)\nEND()\n")
        self.write("dep/dep.cpp", "int dep() { return 42; }\n")
        link = self.node(self.graph(), "answer")
        archives = [arg for command in link["cmds"] for arg in command["cmd_args"] if arg.endswith(".a")]
        runtime = archives.index("tools/enum_parser/enum_serialization_runtime/libtools-enum_parser-enum_serialization_runtime.a")
        self.assertLess(runtime, archives.index(f"{PROJECT}/dep/lib{PROJECT}-dep.a"))
