import json

from tst.make.lib import MakeTest, PROJECT


class GoLinkInputsTest(MakeTest):
    def program(self, macros=""):
        self.write("ya.make", f"GO_PROGRAM(answer)\nSRCS(main.go)\n{macros}\nEND()\n")
        self.write("main.go", "package main\nfunc main() {}\n")

    def test_go_graphs_pass_ownership_checks(self):
        self.program()
        self.node(self.graph(env={"AY_DEBUG_OWNERSHIP": "1"}), "answer")

    def test_go_linker_plugins_keep_the_generated_plugin_input(self):
        self.program("LD_PLUGIN(plugin.py)")
        self.write("plugin.py", "def onlink_cmd(cmd): return cmd\n")
        linked = self.node(self.graph(), "answer")
        args = [arg.replace("$(BUILD_ROOT)", "$(B)")
                for command in linked["cmds"] for arg in command["cmd_args"]]
        self.assertIn(f"$(B)/{PROJECT}/plugin.py.pyplugin", args)

    def test_go_configured_linker_inputs_are_tracked(self):
        self.program()
        config = self.root / "ya.conf"
        flags = '-Wl,-T,${input:"${ARCADIA_ROOT}/' + PROJECT + '/answer.ld"}'
        self.write_tree("ya.conf", config.read_text() + '\n[flags]\nLDFLAGS = ' + json.dumps(flags) + '\n')
        self.write("answer.ld", "SECTIONS {}\n")
        linked = self.node(self.graph(), "answer")
        self.assertIn(f"$(S)/{PROJECT}/answer.ld", linked["inputs"])

    def test_go_linker_input_variables_are_tracked(self):
        self.program()
        config = self.root / "ya.conf"
        flags = {"LDFLAGS": "-Wl,-T,${input:E2E_LINK_SCRIPT}",
                 "E2E_LINK_SCRIPT": "${ARCADIA_ROOT}/" + PROJECT + "/answer.ld"}
        self.write_tree("ya.conf", config.read_text() + '\n[flags]\n' +
                        '\n'.join(name + ' = ' + json.dumps(value) for name, value in flags.items()) + '\n')
        self.write("answer.ld", "SECTIONS {}\n")
        linked = self.node(self.graph(), "answer")
        self.assertIn(f"$(S)/{PROJECT}/answer.ld", linked["inputs"])
