import json

from tst.make.lib import MakeTest


class WindowsBpfTest(MakeTest):
    def test_windows_bpf_invokes_the_platform_wrapper(self):
        config = json.loads((self.root / "build/ya.conf.json").read_text())
        for toolchain in config["toolchain"].values():
            for platform in list(toolchain.get("platforms", [])):
                if platform.get("target", {}).get("os") == "WIN":
                    toolchain["platforms"].append(dict(platform, target=dict(platform["target"], os="WINDOWS")))
        self.write_tree("build/ya.conf.json", json.dumps(config))
        self.write("ya.make", "LIBRARY()\nBPF(answer.c answer.bpf.o)\nRESOURCE(answer.bpf.o /bpf)\nEND()\n")
        self.write("answer.c", "int answer(void) { return 42; }\n")
        generated = self.node(self.graph("--target-platform=default-windows-x86_64"), "answer.bpf.o")
        commands = [command["cmd_args"] for command in generated["cmds"]
                    if any(arg.endswith("/clang_wrapper.py") for arg in command["cmd_args"])]
        self.assertEqual(len(commands), 1)
        index = next(i for i, arg in enumerate(commands[0]) if arg.endswith("/clang_wrapper.py"))
        self.assertEqual(commands[0][index + 1], "yes")
