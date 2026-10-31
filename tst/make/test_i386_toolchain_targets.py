import json

from tst.make.lib import MakeTest


class I386ToolchainTargetsTest(MakeTest):
    def test_i386_targets_compile_libraries_with_a_configured_clang_toolchain(self):
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
        self.assertIsNotNone(selected)
        name, chain, platform = selected
        chain["platforms"] = [platform, dict(platform, target={"os": "LINUX", "arch": "i386"})]
        config["toolchain"] = {key: value for key, value in config["toolchain"].items() if "c++" not in value.get("tools", {}) or key == name}
        self.write_tree("build/ya.conf.json", json.dumps(config))
        self.write("ya.make", "LIBRARY()\nNO_UTIL()\nNO_RUNTIME()\nSET(NEED_PLATFORM_PEERDIRS no)\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        compiled = self.node(self.graph("--target-platform=default-linux-i386", "-DOS_SDK=local"), "answer.cpp.o")
        self.assertIn(f"$(S)/{self.project_name}/answer.cpp", compiled["inputs"])
