from tst.make import toolchains, upstream_rust
from tst.make.lib import MakeTest


class RustWindowsAliasesTest(MakeTest):
    project_name = upstream_rust.PROJECT

    def setUp(self):
        super().setUp()
        upstream_rust.install(self)
        self.write("ya.make", "CRATE(answer)\nEND()\n")
        self.write("src/lib.rs", "pub fn answer() -> u32 { 42 }\n")

    def windows_target(self, architecture):
        toolchains.alias_windows_targets(self, arch=architecture)
        crate = self.node(self.graph("--target-platform=default-windows-" + architecture), "libanswer.lib")
        args = [arg for command in crate["cmds"] for arg in command["cmd_args"]]
        return args[args.index("--target") + 1]

    def test_windows_alias_uses_the_x86_64_rust_target(self):
        self.assertEqual(self.windows_target("x86_64"), "x86_64-pc-windows-msvc")
