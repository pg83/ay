from tst.make.lib import MakeTest


class ModuleInitializationReplayTest(MakeTest):
    def test_replayed_module_initialization_drops_transient_flags(self):
        config = "build/ymake.core.conf"
        original = (self.root / config).read_text()
        self.write_tree(config, original + '\nwhen ($env_BASE_UNIT == "yes" && $envLIBRARY != "yes") {\n    E2E_TRANSIENT_FLAGS = -DE2E_TRANSIENT\n}\n')
        self.write("ya.make", "RECURSE(first second)\n")
        for directory in ("first", "second"):
            self.write(f"{directory}/ya.make", "LIBRARY()\nCFLAGS(${E2E_TRANSIENT_FLAGS} -DE2E_PRESENT)\nSRCS(answer.cpp)\nEND()\n")
            self.write(f"{directory}/answer.cpp", "int answer() { return 42; }\n")
        graph = self.graph()
        for directory in ("first", "second"):
            args = [arg for command in self.node(graph, directory + "/answer.cpp.o")["cmds"] for arg in command["cmd_args"]]
            self.assertIn("-DE2E_PRESENT", args)
            self.assertNotIn("-DE2E_TRANSIENT", args)
