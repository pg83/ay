from tst.make.lib import MakeTest, PROJECT


class PartialGoGeneratorPublicationTest(MakeTest):
    def test_keep_going_preserves_healthy_roots_with_unfilled_generator_inputs(self):
        for cuda_macro in ("SRCS", "SRC"):
            for shared_tool in (False, True):
                with self.subTest(cuda_macro=cuda_macro, shared_tool=shared_tool):
                    self.write("ya.make", "RECURSE(java broken healthy)\n")
                    self.write("java/ya.make", f"JAVA_LIBRARY(subject)\nPEERDIR({PROJECT}/schema)\nJAVA_SRCS(Subject.java)\nEND()\n")
                    self.write("java/Subject.java", "public class Subject {}\n")
                    self.write("schema/ya.make", "FBS_LIBRARY(schema)\nONLY_TAGS(JAVA_FBS)\nSRCS(answer.fbs native.cpp)\nEND()\n")
                    self.write("schema/answer.fbs", "namespace e2e; table Answer { value:int; } root_type Answer;\n")
                    self.write("schema/native.cpp", "int native_answer() { return 42; }\n")
                    self.write("broken/ya.make", f"GO_LIBRARY(broken)\nRUN_PROGRAM({PROJECT}/tool go IN ${{ARCADIA_BUILD_ROOT}}/{PROJECT}/schema/native.cpp.o STDOUT generated.go)\nCOPY_FILE(template.cu kernel.cu)\n{cuda_macro}(kernel.cu)\nEND()\n")
                    self.write("broken/template.cu", "__global__ void kernel(int* value) { *value = 42; }\n")
                    self.write("tool/ya.make", "PROGRAM(generator)\nSRCS(main.cpp)\nEND()\n")
                    self.write("tool/main.cpp", '#include <cstdio>\n#include <cstring>\nint main(int argc, char** argv) { if (argc > 1 && std::strcmp(argv[1], "go") == 0) std::puts("package broken"); else std::puts("int generated() { return 17; }"); }\n')
                    generate = f"RUN_PROGRAM({PROJECT}/tool cpp STDOUT generated.cpp)\n" if shared_tool else ""
                    self.write("healthy/ya.make", f"PROGRAM(answer)\n{generate}SRCS(main.cpp)\nEND()\n")
                    self.write("healthy/main.cpp", "int main() { return 0; }\n")
                    graph = self.graph("-k", env={"AY_DEBUG_OWNERSHIP": "1"})
                    self.assertIn(self.node(graph, "healthy/answer")["uid"], graph["result"])
