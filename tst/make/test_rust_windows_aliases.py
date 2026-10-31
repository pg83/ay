import json

from tst.make import upstream_rust
from tst.make.lib import MakeTest


class RustWindowsAliasesTest(MakeTest):
    def setUp(self):
        super().setUp()
        upstream_rust.install(self)
        self.write("ya.make", "CRATE(answer)\nEND()\n")
        self.write("src/lib.rs", "pub fn answer() -> u32 { 42 }\n")

    def windows_target(self, architecture):
        config = json.loads((self.root / "build/ya.conf.json").read_text())
        for toolchain in config["toolchain"].values():
            for platform in list(toolchain.get("platforms", [])):
                if platform.get("target", {}).get("os") == "WIN":
                    target = dict(platform["target"], os="WINDOWS", arch=architecture)
                    toolchain["platforms"].append(dict(platform, target=target))
        self.write_tree("build/ya.conf.json", json.dumps(config))
        crate = self.node(self.graph("--target-platform=default-windows-" + architecture), "libanswer.lib")
        args = [arg for command in crate["cmds"] for arg in command["cmd_args"]]
        return args[args.index("--target") + 1]

    def test_windows_alias_uses_the_x86_64_rust_target(self):
        self.assertEqual(self.windows_target("x86_64"), "x86_64-pc-windows-msvc")
