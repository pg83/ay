from tst.make.lib import MakeTest


class GoModuleArgumentsTest(MakeTest):
    def test_module_prefixes_are_applied_to_library_and_program_names(self):
        for module, output, source in (("GO_LIBRARY", "e2e_answer.a", "package answer\n"),
                                       ("GO_PROGRAM", "e2e_answer", "package main\nfunc main() {}\n")):
            with self.subTest(module=module):
                self.write("ya.make", f"{module}(answer PREFIX e2e_)\nSRCS(answer.go)\nEND()\n")
                self.write("answer.go", source)
                self.node(self.graph(), output)

    def test_a_missing_prefix_value_does_not_replace_the_module_name(self):
        self.write("ya.make", "GO_LIBRARY(answer PREFIX)\nSRCS(answer.go)\nEND()\n")
        self.write("answer.go", "package answer\n")
        self.node(self.graph(), "answer.a")

    def test_extra_module_arguments_do_not_replace_the_declared_name(self):
        self.write("ya.make", "GO_LIBRARY(answer ignored)\nSRCS(answer.go)\nEND()\n")
        self.write("answer.go", "package answer\n")
        self.node(self.graph(), "answer.a")

    def test_build_embed_arguments_require_one_pattern_and_complete_prefixes(self):
        self.write("answer.go", "package answer\n")
        for args in ("PREFIX", "FILES value.txt", "first second"):
            with self.subTest(args=args):
                self.write("ya.make", f"GO_LIBRARY()\nGO_EMBED_BINDIR({args})\nSRCS(answer.go)\nEND()\n")
                self.assertNotEqual(self.make().returncode, 0)
