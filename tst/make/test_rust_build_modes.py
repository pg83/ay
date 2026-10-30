from tst.make import upstream_rust
from tst.make.lib import MakeTest


class RustBuildModesTest(MakeTest):
    def setUp(self):
        super().setUp()
        upstream_rust.install(self)

    def test_relwithdebinfo_optimizes_and_keeps_debug_information(self):
        self.write("ya.make", "CRATE(answer)\nEND()\n")
        self.write("src/lib.rs", "pub fn answer() -> u32 { 42 }\n")
        node = self.node(self.graph("--build=relwithdebinfo"), "libanswer.a")
        args = [arg for command in node["cmds"] for arg in command["cmd_args"]]
        self.assertEqual(args[args.index("--opt-level") + 1], "3")
        self.assertEqual(args[args.index("--debuginfo") + 1], "2")
