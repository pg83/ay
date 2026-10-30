from tst.make import upstream_rust
from tst.make.lib import MakeTest


class RustPlatformsTest(MakeTest):
    def setUp(self):
        super().setUp()
        upstream_rust.install(self)
        self.write("src/lib.rs", "pub fn answer() -> u32 { 42 }\n")

    def test_named_rust_macros_validate_argument_counts(self):
        for macro, arguments in (("CRATE_BUILD_TOOL", "tool"), ("CRATE_DEPENDENCY_ALIAS", "alias"),
                                 ("CRATE_ROOT", "first second"), ("CRATE_VERSION", "first second")):
            with self.subTest(macro=macro):
                self.write("ya.make", f"CRATE(answer)\n{macro}({arguments})\nEND()\n")
                self.assertNotEqual(self.make().returncode, 0)

    def test_darwin_host_tools_use_darwin_rust_targets_and_suffixes(self):
        for platform, triple in (("default-darwin-x86_64", "x86_64-apple-darwin"),
                                 ("default-darwin-arm64", "aarch64-apple-darwin")):
            with self.subTest(platform=platform):
                self.write("ya.make", "CRATE_PROC_MACRO(answer)\nEND()\n")
                generated = self.node(self.graph("--target-platform=" + platform, "--host-platform=" + platform), "libanswer.dylib")
                args = [arg for command in generated["cmds"] for arg in command["cmd_args"]]
                self.assertEqual(args[args.index("--target") + 1], triple)

    def test_windows_crates_and_host_tools_use_windows_suffixes(self):
        for module, output in (("CRATE", "libanswer.lib"), ("RUST_BUILD_SCRIPT", "answer.exe"),
                               ("CRATE_PROC_MACRO", "libanswer.dll")):
            with self.subTest(module=module):
                self.write("ya.make", f"{module}(answer)\nEND()\n")
                generated = self.node(self.graph("--target-platform=default-win-x86_64", "--host-platform=default-win-x86_64"), output)
                args = [arg for command in generated["cmds"] for arg in command["cmd_args"]]
                self.assertEqual(args[args.index("--target") + 1], "x86_64-pc-windows-msvc")
