from tst.make.lib import MakeTest


class ConfigurationModuleOriginalsTest(MakeTest):
    def test_module_initialization_keeps_each_original_variable_value(self):
        config = "build/ymake.core.conf"
        original = (self.root / config).read_text()
        marker = "module LIBRARY: _LIBRARY {"
        self.assertEqual(original.count(marker), 1)
        self.write_tree(config, original.replace(marker, marker + "\n    E2E_MODULE_FLAGS += -DE2E_INITIALIZED"))
        self.write("ya.make", "RECURSE(base custom repeated)\n")
        for directory, flag in (("base", ""), ("custom", "-DE2E_CUSTOM"), ("repeated", "-DE2E_REPEATED")):
            setting = f"SET(E2E_MODULE_FLAGS {flag})\n" if flag else ""
            self.write(f"{directory}/ya.make", setting + "LIBRARY()\nCFLAGS(${E2E_MODULE_FLAGS})\nSRCS(answer.cpp)\nEND()\n")
            self.write(f"{directory}/answer.cpp", "int answer() { return 42; }\n")
        graph = self.graph()
        for directory, flag in (("base", ""), ("custom", "-DE2E_CUSTOM"), ("repeated", "-DE2E_REPEATED")):
            args = [arg for command in self.node(graph, directory + "/answer.cpp.o")["cmds"] for arg in command["cmd_args"]]
            self.assertEqual(args.count("-DE2E_INITIALIZED"), 1)
            for candidate in ("-DE2E_CUSTOM", "-DE2E_REPEATED"):
                self.assertEqual(args.count(candidate), int(candidate == flag))
