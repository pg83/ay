from tst.make.lib import MakeTest, PROJECT


class VarAsMacroTest(MakeTest):
    def library(self, name):
        self.write(f"{name}/ya.make", "LIBRARY()\nSRCS(lib.cpp)\nEND()\n")
        self.write(f"{name}/lib.cpp", f"int {name}() {{ return 0; }}\n")

    def link_args(self, text):
        self.write("ya.make", text)
        self.write("main.cpp", "int main() { return 0; }\n")
        return [arg for command in self.node(self.graph(), PROJECT)["cmds"] for arg in command["cmd_args"]]

    def archive(self, args, name):
        positions = [i for i, arg in enumerate(args) if f"{PROJECT}/{name}/" in arg and arg.endswith(".a")]
        self.assertTrue(positions, f"no archive of {name} in {args}")
        return positions[0]

    def test_peerdir_variable_set_before_module(self):
        self.library("lib")
        self.archive(self.link_args(f"SET(PEERDIR {PROJECT}/lib)\nPROGRAM()\nSRCS(main.cpp)\nEND()\n"), "lib")

    def test_peerdir_variable_is_applied_before_statements(self):
        for name in ("first", "second", "third"):
            self.library(name)
        args = self.link_args(f"PROGRAM()\nPEERDIR({PROJECT}/first)\nSET_APPEND(PEERDIR {PROJECT}/second)\nPEERDIR({PROJECT}/third)\nSRCS(main.cpp)\nEND()\n")
        self.assertLess(self.archive(args, "second"), self.archive(args, "first"))
        self.assertLess(self.archive(args, "first"), self.archive(args, "third"))

    def test_addincl_variable_is_applied_before_statements(self):
        for name in ("first", "second", "third"):
            self.write(f"{name}/{name}.h", "#pragma once\n")
        self.write("ya.make", f"LIBRARY()\nADDINCL({PROJECT}/first)\nSET_APPEND(ADDINCL {PROJECT}/second)\nADDINCL({PROJECT}/third)\nSRCS(a.cpp)\nEND()\n")
        self.write("a.cpp", '#include "first.h"\n#include "second.h"\n#include "third.h"\nint a() { return 0; }\n')
        args = [arg.replace("$(SOURCE_ROOT)", "$(S)") for command in self.node(self.graph(), "a.cpp.o")["cmds"] for arg in command["cmd_args"]]
        order = [args.index(f"-I$(S)/{PROJECT}/{name}") for name in ("second", "first", "third")]
        self.assertEqual(order, sorted(order))

    def test_addincl_variable_set_before_module(self):
        self.write("inc/answer.h", "#pragma once\n")
        self.write("ya.make", f"SET(ADDINCL {PROJECT}/inc)\nLIBRARY()\nSRCS(a.cpp)\nEND()\n")
        self.write("a.cpp", '#include "answer.h"\nint a() { return 0; }\n')
        args = [arg.replace("$(SOURCE_ROOT)", "$(S)") for command in self.node(self.graph(), "a.cpp.o")["cmds"] for arg in command["cmd_args"]]
        self.assertIn(f"-I$(S)/{PROJECT}/inc", args)
