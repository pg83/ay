from tst.make import upstream_rust
from tst.make.lib import MakeTest
from tst.make.platform_fixtures import add_linux_clang_target


class RustUnsupportedTargetsTest(MakeTest):
    def setUp(self):
        super().setUp()
        upstream_rust.install(self)
        self.write("ya.make", "CRATE(answer)\nEND()\n")
        self.write("src/lib.rs", "pub fn answer() -> u32 { 42 }\n")

    def check_unsupported_target(self, platform):
        crate = self.node(self.graph("--target-platform=" + platform, "-DOS_SDK=local"), "libanswer.a")
        args = [arg for command in crate["cmds"] for arg in command["cmd_args"]]
        self.assertEqual(args[args.index("--target") + 1], "unsupported")

    def test_i386_crates_keep_the_unsupported_rust_target_marker(self):
        add_linux_clang_target(self, "i386")
        self.check_unsupported_target("default-linux-i386")
