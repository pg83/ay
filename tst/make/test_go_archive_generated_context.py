from tst.make.lib import MakeTest, PROJECT


class GoArchiveGeneratedContextTest(MakeTest):
    def test_keep_going_preserves_healthy_roots_with_an_archive_output_collision(self):
        self.write("ya.make", "RECURSE(broken healthy)\n")
        self.write("broken/ya.make", f"GO_LIBRARY(answer)\nSET(DONT_RESOLVE_INCLUDES yes)\nFROM_SANDBOX(FILE 123456789 OUT_NOAUTO template.in OUTPUT_INCLUDES {PROJECT}/broken/generated.h)\nCOPY_FILE(source.h generated.h)\nCOPY_FILE_WITH_CONTEXT(${{BINDIR}}/template.in answer.a)\nRUN_PROGRAM({PROJECT}/tool IN ${{BINDIR}}/answer.a STDOUT generated.go)\nEND()\n")
        self.write("broken/source.h", "#define ANSWER 42\n")
        self.write("tool/ya.make", "PROGRAM(generator)\nSRCS(main.cpp)\nEND()\n")
        self.write("tool/main.cpp", '#include <cstdio>\nint main() { std::puts("package answer"); }\n')
        self.write("healthy/ya.make", "PROGRAM(answer)\nSRCS(main.cpp)\nEND()\n")
        self.write("healthy/main.cpp", "int main() { return 0; }\n")
        graph = self.graph("-k")
        self.assertIn(self.node(graph, "healthy/answer")["uid"], graph["result"])
