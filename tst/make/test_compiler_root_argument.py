from tst.make.lib import MakeTest


class CompilerRootArgumentTest(MakeTest):
    def test_compiler_flags_can_pass_the_source_root_as_a_separate_argument(self):
        self.write("ya.make", "LIBRARY()\nCFLAGS(-iquote ${ARCADIA_ROOT})\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        node = self.node(self.graph(), "answer.cpp.o")
        arguments = [arg.replace("$(SOURCE_ROOT)", "$(S)") for command in node["cmds"] for arg in command["cmd_args"]]
        self.assertEqual(arguments[arguments.index("-iquote") + 1], "$(S)")
