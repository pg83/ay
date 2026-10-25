from tst.make.lib import MakeTest, PROJECT


class RustTest(MakeTest):
    def consumer(self, directory):
        self.write("ya.make", f"PROGRAM()\nSRCS(main.cpp)\nPEERDIR({PROJECT}/{directory})\nEND()\n")
        self.write("main.cpp", "int main() { return 0; }\n")

    def crate(self, directory, name, macros="", module="CRATE"):
        self.write(f"{directory}/ya.make", f"{module}({name})\n{macros}\nEND()\n")
        self.write(f"{directory}/src/lib.rs", "pub fn answer() -> u32 { 42 }\n")

    def args(self, node):
        return [arg.replace("$(SOURCE_ROOT)", "$(S)").replace("$(BUILD_ROOT)", "$(B)")
                for command in node["cmds"] for arg in command["cmd_args"]]

    def section(self, node, option):
        args = self.args(node)
        start = args.index(option) + 1
        end = next((i for i in range(start, len(args)) if args[i].startswith("--")), len(args))
        return args[start:end]

    def test_root_modules(self):
        for module, output, kind in (("CRATE", "libanswer.a", "rlib"),
                                     ("RUST_RUNTIME", "libanswer.a", "runtime"),
                                     ("RUST_BUILD_SCRIPT", "answer", "bin"),
                                     ("CRATE_PROC_MACRO", "libanswer.so", "proc-macro")):
            with self.subTest(module=module):
                self.crate(".", "answer", module=module)
                graph = self.graph()
                node = self.node(graph, output)
                self.assertIn(node["uid"], graph["result"])
                self.assertEqual(self.section(node, "--kind"), [kind])
                self.assertEqual(self.section(node, "--version"), ["0.0.0"])
                self.assertEqual(self.section(node, "--edition"), ["2021"])
                self.assertEqual(self.section(node, "--opt-level"), ["0"])
                self.assertEqual(self.section(node, "--debuginfo"), ["2"])

    def test_crate_macros(self):
        self.consumer("pkg")
        self.crate("pkg", "answer", """
CRATE_ROOT(source.rs)
CRATE_VERSION(1.2.3)
CRATE_EDITION(2024)
CRATE_MANIFEST(Cargo.toml)
CRATE_MANIFEST_DIR(..)
CRATE_SOURCES(extra.rs)
SRCS(alias.rs)
CRATE_INPUTS(data.txt)
CRATE_FEATURES(first)
CRATE_FEATURES(second)
CRATE_FLAGS(-Copt-level=1 --cfg=e2e)
CRATE_ENV(FIRST=1)
CRATE_ENV(SECOND=2)
""")
        for file in ("source.rs", "extra.rs", "alias.rs"):
            self.write(f"pkg/{file}", "pub fn answer() -> u32 { 42 }\n")
        self.write("pkg/data.txt", "42\n")
        self.write("pkg/Cargo.toml", '[package]\nname = "answer"\nversion = "1.2.3"\n')
        graph = self.graph("-r")
        node = self.node(graph, "pkg/libanswer.a")
        self.assertEqual(set(node["outputs"]), {
            f"$(B)/{PROJECT}/pkg/libanswer{suffix}" for suffix in (".a", ".rlib", ".build.tar")})
        for file in ("source.rs", "extra.rs", "alias.rs", "data.txt", "Cargo.toml"):
            self.assertIn(f"$(S)/{PROJECT}/pkg/{file}", node["inputs"])
        for option, value in (("--version", "1.2.3"), ("--edition", "2024"),
                              ("--manifest-dir", f"$(S)/{PROJECT}/pkg/.."),
                              ("--opt-level", "3"), ("--debuginfo", "0"),
                              ("--kind", "rlib"), ("--mode", "target")):
            self.assertEqual(self.section(node, option), [value])
        self.assertEqual(self.section(node, "--features"), ["first", "second"])
        self.assertEqual(self.section(node, "--env"), ["FIRST=1", "SECOND=2"])
        self.assertIn("--flag=-Copt-level=1", self.args(node))
        self.assertIn("--flag=--cfg=e2e", self.args(node))
        self.assertFalse(any("-host.a" in output for n in graph["graph"] for output in n["outputs"]))

    def host_fixture(self):
        self.consumer("api")
        self.crate("common", "common")
        self.crate("macros", "derive_answer", f"PEERDIR({PROJECT}/common)", "CRATE_PROC_MACRO")
        self.crate("script", "build_answer", f"CRATE_BUILD_DEPS({PROJECT}/common)", "RUST_BUILD_SCRIPT")
        self.crate("tool", "helper", module="RUST_BUILD_SCRIPT")
        self.crate("api", "answer", f"""
PEERDIR({PROJECT}/common)
CRATE_DEPENDENCY_ALIAS(renamed {PROJECT}/common)
CRATE_BUILD_SCRIPT({PROJECT}/script)
CRATE_PROC_MACROS({PROJECT}/macros)
CRATE_BUILD_TOOL(HELPER {PROJECT}/tool)
""")

    def test_host_tools_and_target_peers(self):
        self.host_fixture()
        graph = self.graph()
        api = self.node(graph, "api/libanswer.a")
        common = self.node(graph, "common/libcommon.a")
        host = self.node(graph, "common/libcommon-host.a")
        script = self.node(graph, "script/build_answer")
        macro = self.node(graph, "macros/libderive_answer.so")
        tool = self.node(graph, "tool/helper")
        self.assertEqual(self.section(api, "--direct"), [common["outputs"][0]])
        self.assertEqual(self.section(api, "--aliases"), [f"renamed={PROJECT}/common"])
        self.assertEqual(self.section(api, "--proc-macros"), [macro["outputs"][0]])
        self.assertEqual(self.section(api, "--build-script-program"), [script["outputs"][0]])
        self.assertIn(f"HELPER={tool['outputs'][0]}", self.section(api, "--env"))
        for node in (script, macro):
            self.assertEqual(self.section(node, "--direct"), [host["outputs"][0]])
        for node in (script, macro, tool, host):
            self.assertEqual(self.section(node, "--mode"), ["host"])
        for node in (script, macro, tool, common):
            self.assertIn(node["uid"], api["deps"])
        runtime = [n for n in graph["graph"] if "--kind" in self.args(n) and self.section(n, "--kind") == ["runtime"]]
        self.assertEqual(len(runtime), 1)
        self.assertEqual(len(runtime[0]["outputs"]), 1)

    def test_cross_target_and_musl_host(self):
        self.host_fixture()
        graph = self.graph("--target-platform", "default-linux-aarch64", "--musl",
                           "--host-platform", "default-linux-x86_64", "--host-platform-flag=MUSL=yes")
        for output in ("api/libanswer.a", "common/libcommon.a"):
            self.assertEqual(self.section(self.node(graph, output), "--target"), ["aarch64-unknown-linux-musl"])
        for output in ("common/libcommon-host.a", "script/build_answer", "macros/libderive_answer.so", "tool/helper"):
            self.assertEqual(self.section(self.node(graph, output), "--target"), ["x86_64-unknown-linux-gnu"])

    def test_direct_template_inputs(self):
        self.consumer("top")
        self.crate("leaf", "leaf", "CRATE_INPUTS(config.h.in data.txt)")
        self.write("leaf/config.h.in", "#define ANSWER @ANSWER@\n")
        self.write("leaf/data.txt", "42\n")
        self.crate("middle", "middle", f"PEERDIR({PROJECT}/leaf)")
        self.crate("top", "top", f"PEERDIR({PROJECT}/middle)")
        graph = self.graph()
        middle = self.node(graph, "middle/libmiddle.a")
        top = self.node(graph, "top/libtop.a")
        self.assertIn(f"$(S)/{PROJECT}/leaf/config.h.in", middle["inputs"])
        self.assertNotIn(f"$(S)/{PROJECT}/leaf/config.h.in", top["inputs"])
        self.assertNotIn(f"$(S)/{PROJECT}/leaf/data.txt", middle["inputs"])

    def test_memory_sanitizer_runtime_peer(self):
        self.host_fixture()
        graph = self.graph("-DSANITIZER_TYPE=memory")
        api = self.node(graph, "api/libanswer.a")
        host = self.node(graph, "common/libcommon-host.a")
        self.assertIn("--sanitizer=memory", self.args(api))
        self.assertTrue(any("/contrib/restricted/rust_msan/" in path for path in self.section(api, "--native")))
        self.assertFalse(any("/contrib/restricted/rust_msan/" in path for path in host["inputs"]))

    def test_native_link_and_shared_runtime(self):
        self.write("ya.make", f"PROGRAM(answer)\nSRCS(main.cpp)\nPEERDIR({PROJECT}/first {PROJECT}/second)\nEND()\n")
        self.write("main.cpp", "int main() { return 0; }\n")
        self.write("native/ya.make", "LIBRARY(native)\nSRCS(native.cpp)\nEND()\n")
        self.write("native/native.cpp", "int answer() { return 42; }\n")
        self.crate("first", "first", f"PEERDIR({PROJECT}/native)")
        self.crate("second", "second")
        graph = self.graph()
        link = self.node(graph, "answer")
        args = self.args(link)
        for output in ("first/libfirst.a", "second/libsecond.a", "native/libnative.a"):
            self.assertIn(f"{PROJECT}/{output}", args)
        self.assertEqual(args.count("build/rust/staticlib/libarcadia_rust_runtime.a"), 1)
        self.assertFalse(any(arg.endswith((".rlib", ".build.tar")) for arg in args))
        native = self.node(graph, "native/libnative.a")
        self.assertIn(native["outputs"][0], self.section(self.node(graph, "first/libfirst.a"), "--native"))


if __name__ == "__main__":
    import unittest
    unittest.main()
