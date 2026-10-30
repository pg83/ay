from tst.make.lib import MakeTest


class RepeatedIncludeOnceTest(MakeTest):
    def test_repeated_include_once_declarations_keep_one_include_effect(self):
        self.write("ya.make", "LIBRARY()\nSET(E2E_INCLUDE flags.inc)\nINCLUDE(${E2E_INCLUDE})\nINCLUDE(${E2E_INCLUDE})\nSRCS(answer.cpp)\nEND()\n")
        self.write("flags.inc", "INCLUDE_ONCE()\nINCLUDE_ONCE()\nCFLAGS(-DE2E_ONCE=42)\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        node = self.node(self.graph(), "answer.cpp.o")
        args = [arg for command in node["cmds"] for arg in command["cmd_args"]]
        self.assertEqual(args.count("-DE2E_ONCE=42"), 1)
