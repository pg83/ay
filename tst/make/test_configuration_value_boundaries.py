import json

from tst.make.lib import MakeTest


class ConfigurationValueBoundariesTest(MakeTest):
    def setUp(self):
        super().setUp()
        self.write("ya.make", "LIBRARY()\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")

    def test_malformed_root_configuration_is_ignored(self):
        self.write_tree("ya.conf", "[flags\n")
        self.node(self.graph(), "answer.cpp.o")

    def test_toolchain_flags_require_scalar_values(self):
        config = json.loads((self.root / "build/ya.conf.json").read_text())
        for chain in config["toolchain"].values():
            if "c++" in chain.get("tools", {}):
                chain.setdefault("params", {}).setdefault("extra_flags", {})["E2E_INVALID"] = ["value"]
        self.write_tree("build/ya.conf.json", json.dumps(config))
        self.assertNotEqual(self.make().returncode, 0)
