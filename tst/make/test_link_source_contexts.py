from tst.make.lib import MakeTest, PROJECT


class LinkSourceContextsTest(MakeTest):
    def test_global_suppression_files_are_transitive_link_inputs(self):
        self.write("ya.make", f"PROGRAM(answer)\nSRCS(main.cpp)\nPEERDIR({PROJECT}/lib)\nEND()\n")
        self.write("main.cpp", "int main() { return 0; }\n")
        self.write("lib/ya.make", "LIBRARY()\nGLOBAL_SRCS(options.supp)\nSRCS(value.cpp)\nEND()\n")
        self.write("lib/value.cpp", "int value() { return 42; }\n")
        self.write("lib/options.supp", '#include "details.h"\n')
        self.write("lib/details.h", "#define E2E_OPTIONS 42\n")
        inputs = self.node(self.graph(), "answer")["inputs"]
        self.assertIn(f"$(S)/{PROJECT}/lib/options.supp", inputs)
        self.assertNotIn(f"$(S)/{PROJECT}/lib/details.h", inputs)

    def test_unrecognized_global_source_does_not_abort_generation(self):
        self.write("unsupported.e2e_unknown", "unsupported\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        for sources in ("", "SRCS(answer.cpp)\n"):
            with self.subTest(sources=sources):
                self.write("ya.make", "LIBRARY()\nGLOBAL_SRCS(unsupported.e2e_unknown)\n" + sources + "END()\n")
                result = self.make()
                self.assertEqual(result.returncode, 0, result.stderr)

    def test_rooted_and_generated_exports_script(self):
        self.write("answer.cpp", "int main() { return 0; }\n")
        self.write("symbols.symlist", "{ global: main; local: *; };\n")
        for generated in (False, True):
            with self.subTest(generated=generated):
                script = "${BINDIR}/generated.symlist" if generated else f"${{ARCADIA_ROOT}}/{PROJECT}/symbols.symlist"
                producer = "COPY_FILE(symbols.symlist generated.symlist)\n" if generated else ""
                self.write("ya.make", f"PROGRAM(answer)\nSRCS(answer.cpp)\n{producer}EXPORTS_SCRIPT({script})\nEND()\n")
                graph = self.graph()
                node = self.node(graph, "answer")
                expected = f"$(B)/{PROJECT}/generated.symlist" if generated else f"$(S)/{PROJECT}/symbols.symlist"
                self.assertIn(expected, node["inputs"])
                args = [arg.replace("$(SOURCE_ROOT)", "$(S)").replace("$(BUILD_ROOT)", "$(B)") for command in node["cmds"] for arg in command["cmd_args"]]
                self.assertTrue(any(expected in arg for arg in args))
                if generated:
                    self.assertIn(self.node(graph, "generated.symlist")["uid"], node["deps"])
