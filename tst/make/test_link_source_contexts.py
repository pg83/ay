from tst.make.lib import MakeTest, PROJECT


class LinkSourceContextsTest(MakeTest):
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
