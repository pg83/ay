from tst.make.lib import MakeTest


class GeneratedRagelInputTest(MakeTest):
    def test_generated_ragel_inputs_keep_their_producer_dependency(self):
        self.write("ya.make", "LIBRARY()\nCOPY_FILE(template.rl6 machine.rl6)\nSRCS(machine.rl6)\nEND()\n")
        self.write("template.rl6", "%%{ machine scanner; main := any*; }%%\n%% write data;\n")
        graph = self.graph()
        generated = self.node(graph, "machine.rl6")
        ragel = self.node(graph, "machine.rl6.cpp")
        self.assertIn(f"$(B)/{self.project_name}/machine.rl6", ragel["inputs"])
        self.assertIn(generated["uid"], ragel["deps"])
