from tst.make.lib import MakeTest, PROJECT


class GoGeneratorPeersTest(MakeTest):
    def test_generator_library_peer_is_not_linked_as_a_cgo_archive(self):
        self.write("ya.make", f"GO_PROGRAM(answer)\nPEERDIR({PROJECT}/generator)\nSRCS(main.go)\nEND()\n")
        self.write("main.go", "package main\nfunc main() {}\n")
        self.write("generator/ya.make", "GEN_LIBRARY(generator)\nRUN_PYTHON3(generate.py OUT_NOAUTO value.txt)\nEND()\n")
        self.write("generator/generate.py", "print(42)\n")
        linked = self.node(self.graph(), "answer")
        args = [arg for command in linked["cmds"] for arg in command["cmd_args"]]
        self.assertFalse(any("libgenerator.a" in arg for arg in args))
