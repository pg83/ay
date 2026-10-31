from tst.make.lib import MakeTest, PROJECT


class TargetRuntimeHostToolsTest(MakeTest):
    def test_target_runtime_flags_preserve_host_generator_dependencies(self):
        self.write("ya.make", f"LIBRARY()\nRUN_PROGRAM({PROJECT}/generator OUT generated.cpp)\nEND()\n")
        self.write("generator/ya.make", "PROGRAM(generator)\nSRCS(main.cpp)\nEND()\n")
        self.write("generator/main.cpp", '#include <cstdio>\nint main() { std::puts("int answer() { return 42; }"); }\n')
        graph = self.graph("--target-platform-flag", "USE_ARCADIA_COMPILER_RUNTIME=yes")
        generated = self.node(graph, "generated.cpp")
        generator = self.node(graph, "generator/generator")
        self.assertIn(generator["uid"], generated["deps"])
        self.assertIn(generated["uid"], self.node(graph, "generated.cpp.o")["deps"])
