import json

from tst.make.lib import MakeTest


class ToolchainPlatformDefaultsTest(MakeTest):
    def test_a_missing_target_platform_defaults_to_the_selected_linux_host(self):
        config = json.loads((self.root / "build/ya.conf.json").read_text())
        selected = None
        for name, chain in config["toolchain"].items():
            if "c++" not in chain.get("tools", {}) or chain.get("params", {}).get("type") != "clang":
                continue
            for platform in chain["platforms"]:
                if platform.get("default") and platform.get("host", {}).get("os") == "LINUX" and platform.get("target") == {"os": "LINUX", "arch": "x86_64"}:
                    selected = name, chain, platform
                    break
            if selected is not None:
                break
        self.assertIsNotNone(selected, "fixture tree needs a default Linux C++ toolchain")
        name, chain, platform = selected
        chain["platforms"] = [dict(platform)]
        del chain["platforms"][0]["target"]
        config["toolchain"] = {key: value for key, value in config["toolchain"].items() if "c++" not in value.get("tools", {}) or key == name}
        self.write_tree("build/ya.conf.json", json.dumps(config))
        self.write("ya.make", "LIBRARY()\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        compiled = self.node(self.graph("-DOS_SDK=local"), "answer.cpp.o")
        self.assertIn(f"$(S)/{self.project_name}/answer.cpp", compiled["inputs"])
