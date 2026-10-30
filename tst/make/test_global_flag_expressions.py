from tst.make.lib import MakeTest, PROJECT


class GlobalFlagExpressionsTest(MakeTest):
    def test_global_cflags_expands_variables_for_the_link_command(self):
        self.write("ya.make", f"PROGRAM(answer)\nPEERDIR({PROJECT}/peer)\nSRCS(main.cpp)\nEND()\n")
        self.write("main.cpp", "int main() { return 0; }\n")
        self.write("peer/ya.make", "LIBRARY(peer)\nSET(E2E_FLAGS -DE2E_FIRST=42 -DE2E_SECOND=43)\nGLOBAL_CFLAGS($E2E_FLAGS)\nSRCS(value.cpp)\nEND()\n")
        self.write("peer/value.cpp", "int value() { return 42; }\n")
        graph = self.graph()
        args = [arg for command in self.node(graph, "answer")["cmds"] for arg in command["cmd_args"]]
        self.assertIn("-DE2E_FIRST=42", args)
        self.assertNotIn("-DE2E_SECOND=43", args)
        args = [arg for command in self.node(graph, "peer/value.cpp.o")["cmds"] for arg in command["cmd_args"]]
        self.assertIn("-DE2E_FIRST=42", args)
        self.assertIn("-DE2E_SECOND=43", args)
