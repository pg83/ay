"""Private C++ toolchain target aliases for CLI fixtures."""

import json


def add_linux_clang_target(test, architecture):
    config = json.loads((test.root / "build/ya.conf.json").read_text())
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
    test.assertIsNotNone(selected)
    name, chain, platform = selected
    chain["platforms"] = [platform, dict(platform, target={"os": "LINUX", "arch": architecture})]
    config["toolchain"] = {key: value for key, value in config["toolchain"].items() if "c++" not in value.get("tools", {}) or key == name}
    test.write_tree("build/ya.conf.json", json.dumps(config))
