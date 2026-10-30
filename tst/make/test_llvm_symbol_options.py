from tst.make.lib import MakeTest


class LlvmSymbolOptionsTest(MakeTest):
    def test_symbol_list_ends_at_the_next_keyword(self):
        self.write("ya.make", "LIBRARY()\nUSE_LLVM_BC16()\nLLVM_BC(answer.cpp SYMBOLS answer NAME answer GENERATE_MACHINE_CODE)\nEND()\n")
        self.write("answer.cpp", 'extern "C" int answer() { return 42; }\n')
        graph = self.graph()
        optimized = self.node(graph, "answer_optimized.bc")
        compiled = self.node(graph, "answer_optimized.bc.o")
        self.assertIn(optimized["uid"], compiled["deps"])
        args = [arg for command in optimized["cmds"] for arg in command["cmd_args"]]
        self.assertTrue(any("answer" in arg and "public-api" in arg for arg in args), args)
