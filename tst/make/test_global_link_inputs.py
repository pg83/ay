from tst.make.lib import MakeTest, PROJECT


class GlobalLinkInputsTest(MakeTest):
    def test_global_archive_and_linker_script_reach_the_link_command(self):
        self.write("ya.make", "PROGRAM(answer)\nSRCS(main.cpp)\nGLOBAL_SRCS(prebuilt.a layout.ld)\nEND()\n")
        self.write("main.cpp", "int main() { return 0; }\n")
        self.write("prebuilt.a", "!<arch>\n")
        self.write("layout.ld", "SECTIONS {}\n")
        linked = self.node(self.graph(), "answer")
        for name in ("prebuilt.a", "layout.ld"):
            self.assertIn(f"$(S)/{PROJECT}/{name}", linked["inputs"])
        args = [arg.replace("$(SOURCE_ROOT)", "$(S)") for command in linked["cmds"] for arg in command["cmd_args"]]
        self.assertIn(f"{PROJECT}/prebuilt.a", args)
        self.assertIn(f"-T$(S)/{PROJECT}/layout.ld", args)

    def test_own_and_peer_global_inputs_are_kept_together(self):
        self.write("ya.make", f"PROGRAM(answer)\nSRCS(main.cpp)\nGLOBAL_SRCS(own.supp)\nPEERDIR({PROJECT}/peer)\nEND()\n")
        self.write("main.cpp", "int main() { return 0; }\n")
        self.write("own.supp", "own suppression\n")
        self.write("peer/ya.make", "LIBRARY()\nGLOBAL_SRCS(peer.supp)\nEND()\n")
        self.write("peer/peer.supp", "peer suppression\n")
        linked = self.node(self.graph(), "answer")
        for name in ("own.supp", "peer/peer.supp"):
            self.assertIn(f"$(S)/{PROJECT}/{name}", linked["inputs"])
