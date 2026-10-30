from tst.make.lib import MakeTest, PROJECT


class PrecompiledSourcesTest(MakeTest):
    def test_source_objects_are_link_inputs_alongside_compiled_sources(self):
        self.write("ya.make", "PROGRAM(answer)\nSRCS(first.o main.cpp second.obj third.o)\nEND()\n")
        self.write("main.cpp", "int main() { return 0; }\n")
        for name in ("first.o", "second.obj", "third.o"):
            self.write(name, "")
        graph = self.graph()
        link = self.node(graph, "answer")
        compiled = self.node(graph, "main.cpp.o")
        for name in ("first.o", "second.obj", "third.o"):
            self.assertIn(f"$(S)/{PROJECT}/{name}", link["inputs"])
        self.assertIn(f"$(B)/{PROJECT}/main.cpp.o", link["inputs"])
        self.assertIn(compiled["uid"], link["deps"])
        self.assertNotIn("", link["deps"])

    def test_generated_object_keeps_its_producer_as_a_link_dependency(self):
        self.write("ya.make", "PROGRAM(answer)\nSRCS(main.cpp)\nRUN_PYTHON3(generate.py OUT generated.o)\nEND()\n")
        self.write("main.cpp", "int main() { return 0; }\n")
        self.write("generate.py", 'from pathlib import Path\nPath("generated.o").write_bytes(b"")\n')
        graph = self.graph()
        generated = self.node(graph, "generated.o")
        link = self.node(graph, "answer")
        self.assertIn(f"$(B)/{PROJECT}/generated.o", link["inputs"])
        self.assertIn(generated["uid"], link["deps"])

    def test_global_source_objects_are_exported_from_a_library(self):
        self.write("ya.make", f"PROGRAM(answer)\nSRCS(main.cpp)\nPEERDIR({PROJECT}/library)\nEND()\n")
        self.write("main.cpp", "int main() { return 0; }\n")
        self.write("library/ya.make", "LIBRARY(objects)\nSRCS(GLOBAL first.o GLOBAL second.obj)\nEND()\n")
        self.write("library/first.o", "")
        self.write("library/second.obj", "")
        graph = self.graph()
        archive = self.node(graph, "library/libobjects.global.a")
        for name in ("first.o", "second.obj"):
            self.assertIn(f"$(S)/{PROJECT}/library/{name}", archive["inputs"])
        self.assertIn(archive["uid"], self.node(graph, "answer")["deps"])

    def test_unproduced_build_object_is_omitted_from_the_link(self):
        self.write("ya.make", "PROGRAM(answer)\nSRCS(main.cpp ${BINDIR}/missing.o)\nEND()\n")
        self.write("main.cpp", "int main() { return 0; }\n")
        graph = self.graph()
        link = self.node(graph, "answer")
        self.assertNotIn(f"$(B)/{PROJECT}/missing.o", link["inputs"])
        self.assertIn(self.node(graph, "main.cpp.o")["uid"], link["deps"])

    def test_noauto_generated_object_requires_registration(self):
        self.write("ya.make", "PROGRAM(answer)\nSRCS(main.cpp ${BINDIR}/generated.o)\nRUN_PYTHON3(generate.py OUT_NOAUTO generated.o)\nEND()\n")
        self.write("main.cpp", "int main() { return 0; }\n")
        self.write("generate.py", 'from pathlib import Path\nPath("generated.o").write_bytes(b"")\n')
        result = self.make()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("generated.o", result.stderr)

    def test_src_rejects_a_header_that_has_no_compilation_output(self):
        self.write("ya.make", "LIBRARY(answer)\nSRC(answer.h)\nEND()\n")
        self.write("answer.h", "#pragma once\n")
        result = self.make()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("no outputs", result.stderr)
