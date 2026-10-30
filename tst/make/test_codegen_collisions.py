from tst.make.lib import MakeTest, PROJECT


class CodegenCollisionsTest(MakeTest):
    def test_two_generators_cannot_publish_the_same_output(self):
        self.write("ya.make", "LIBRARY()\nRUN_PYTHON3(first.py OUT answer.cpp)\nRUN_PYTHON3(second.py OUT answer.cpp)\nEND()\n")
        for name in ("first", "second"):
            self.write(name + ".py", 'from pathlib import Path\nPath("answer.cpp").write_text("int answer() { return 42; }\\n")\n')
        result = self.make()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("answer.cpp", result.stderr)

    def test_unused_peer_generator_does_not_conflict_with_a_shared_header(self):
        self.write("ya.make", f"PROGRAM(answer)\nADDINCL(${{ARCADIA_BUILD_ROOT}}/{PROJECT})\nSRCS(main.cpp)\nPEERDIR({PROJECT}/first {PROJECT}/second)\nEND()\n")
        self.write("main.cpp", '#include "shared.h"\nint main() { return VALUE; }\n')
        for name in ("first", "second"):
            self.write(f"{name}/ya.make", f"LIBRARY()\nCOPY_FILE(template.h ${{ARCADIA_BUILD_ROOT}}/{PROJECT}/shared.h)\nSRCS(value.cpp)\nEND()\n")
            self.write(f"{name}/template.h", "#define VALUE 42\n")
            self.write(f"{name}/value.cpp", f"int {name}() {{ return 42; }}\n")
        graph = self.graph()
        generated = self.node(graph, "shared.h")
        compiled = self.node(graph, "main.cpp.o")
        self.assertIn(f"$(B)/{PROJECT}/shared.h", compiled["inputs"])
        self.assertIn(generated["uid"], compiled["deps"])
        for name in ("first", "second"):
            self.node(graph, f"{name}/value.cpp.o")
