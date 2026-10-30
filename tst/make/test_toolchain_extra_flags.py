import json

from tst.make.lib import MakeTest


class ToolchainExtraFlagsTest(MakeTest):
    def test_toolchain_flags_are_available_to_module_configuration(self):
        config = json.loads((self.root / "build/ya.conf.json").read_text())
        for chain in config["toolchain"].values():
            if "c++" in chain.get("tools", {}):
                chain.setdefault("params", {}).setdefault("extra_flags", {})["E2E_TOOLCHAIN_FLAG"] = "42"
        self.write_tree("build/ya.conf.json", json.dumps(config))
        self.write("ya.make", "LIBRARY()\nCFLAGS(-DE2E_TOOLCHAIN_FLAG=${E2E_TOOLCHAIN_FLAG})\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        node = self.node(self.graph(), "answer.cpp.o")
        args = [arg for command in node["cmds"] for arg in command["cmd_args"]]
        self.assertIn("-DE2E_TOOLCHAIN_FLAG=42", args)
