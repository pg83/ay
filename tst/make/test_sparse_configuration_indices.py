from tst.make.lib import MakeTest


class SparseConfigurationIndicesTest(MakeTest):
    def test_conditional_flags_survive_a_large_gap_of_constant_variables(self):
        core = self.root / "build/ymake.core.conf"
        padding = "".join(f"E2E_PADDING_{index:04d}=yes\n" for index in range(4096))
        conditional = '\nwhen ($OS_LINUX == "yes") {\n    E2E_SPARSE_FLAG = -DE2E_SPARSE=42\n}\n'
        self.write_tree("build/ymake.core.conf", core.read_text() + "\n" + padding + conditional)
        self.write("ya.make", "LIBRARY()\nCFLAGS($E2E_SPARSE_FLAG)\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        node = self.node(self.graph(), "answer.cpp.o")
        args = [arg for command in node["cmds"] for arg in command["cmd_args"]]
        self.assertIn("-DE2E_SPARSE=42", args)
