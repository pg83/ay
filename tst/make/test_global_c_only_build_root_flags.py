from tst.make.lib import MakeTest


class GlobalCOnlyBuildRootFlagsTest(MakeTest):
    def test_global_c_flags_keep_a_separate_build_root_operand(self):
        self.write("ya.make", f"PROGRAM(answer)\nPEERDIR({self.project_name}/peer)\nSRCS(main.c)\nEND()\n")
        self.write("main.c", "int main(void) { return 0; }\n")
        self.write("peer/ya.make", "LIBRARY(peer)\nCONLYFLAGS(GLOBAL -iquote GLOBAL ${ARCADIA_BUILD_ROOT}/)\nSRCS(peer.c)\nEND()\n")
        self.write("peer/peer.c", "int answer(void) { return 42; }\n")
        graph = self.graph()
        for output in ("main.c.o", "answer"):
            with self.subTest(output=output):
                compiled = self.node(graph, output)
                arguments = [arg.replace("$(BUILD_ROOT)", "$(B)") for command in compiled["cmds"] for arg in command["cmd_args"]]
                self.assertEqual(arguments[arguments.index("-iquote") + 1].rstrip("/"), "$(B)")
