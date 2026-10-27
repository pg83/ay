from tst.make.lib import MakeTest, PROJECT


class ProgramTest(MakeTest):
    def test_program_links_peer_library(self):
        self.write("ya.make", f"PROGRAM(hello)\nPEERDIR({PROJECT}/math)\nSRCS(main.cpp)\nEND()\n")
        self.write("main.cpp", f'#include <{PROJECT}/math/answer.h>\nint main() {{ return answer() != 42; }}\n')
        self.write("math/ya.make", "LIBRARY()\nSRCS(answer.cpp)\nEND()\n")
        self.write("math/answer.h", "int answer();\n")
        self.write("math/answer.cpp", '#include "answer.h"\nint answer() { return 42; }\n')
        graph = self.graph()
        self.assertIn(f"$(S)/{PROJECT}/math/answer.h", self.node(graph, "main.cpp.o")["inputs"])
        self.node(graph, "math/answer.cpp.o")
        executable = self.node(graph, "hello")
        self.assertIn(executable["uid"], graph["result"])
        self.assertTrue(any(path.endswith(".a") and f"/{PROJECT}/math/" in path for path in executable["inputs"]))

    def test_conf_macro_ldflags_dollar_literal(self):
        self.write("ya.make", "PROGRAM(hello)\nUSE_DYNAMIC_CUDA()\nSRCS(main.cpp)\nEND()\n")
        self.write("main.cpp", "int main() { return 0; }\n")
        args = [arg for cmd in self.node(self.graph(), "hello")["cmds"] for arg in cmd["cmd_args"]]
        self.assertIn("-Wl,-rpath,$ORIGIN", args)
