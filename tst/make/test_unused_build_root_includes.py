from tst.make.lib import MakeTest, PROJECT


class UnusedBuildRootIncludesTest(MakeTest):
    def test_unused_rooted_header_includes_do_not_block_independent_compilation(self):
        self.write("ya.make", "RECURSE(generator consumer)\n")
        self.write("generator/ya.make", f"LIBRARY(generator)\nRUN_PROGRAM({PROJECT}/tool OUT_NOAUTO generated.h OUTPUT_INCLUDES ${{ARCADIA_BUILD_ROOT}}/{PROJECT}/missing.h)\nRUN_PROGRAM({PROJECT}/tool STDOUT generated.cpp)\nSRCS(seed.cpp)\nEND()\n")
        self.write("tool/ya.make", "PROGRAM(generator)\nSRCS(main.cpp)\nEND()\n")
        self.write("tool/main.cpp", "int main() { return 0; }\n")
        self.write("generator/seed.cpp", "int seed() { return 17; }\n")
        self.write("consumer/ya.make", "LIBRARY(answer)\nSRCS(public.h answer.cpp)\nEND()\n")
        self.write("consumer/public.h", f'#include <{PROJECT}/generator/generated.h>\n')
        self.write("consumer/answer.cpp", "int answer() { return 42; }\n")
        graph = self.graph()
        self.node(graph, "consumer/answer.cpp.o")
        self.node(graph, "generator/seed.cpp.o")
        self.assertIn(self.node(graph, "generator/generated.cpp")["uid"], self.node(graph, "generator/generated.cpp.o")["deps"])
