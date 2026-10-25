import unittest

from tst import lib


class ImplibTest(unittest.TestCase):
    def fixture(self, invocation):
        files = {
            "mod/ya.make": f"LIBRARY()\n{lib.BARE_MODULE}{invocation}\nEND()\n",
            "app/ya.make": f"PROGRAM()\n{lib.BARE_MODULE}PEERDIR(mod)\nSRCS(main.cpp)\nEND()\n",
            "app/main.cpp": "int main() {}\n",
        }
        lib.tool_program(files, "contrib/tools/implib", "implib")
        return files

    def test_toolkit_wrapper_compiles_both_generated_sources(self):
        files = self.fixture("GENERATE_IMPLIB(driver $CUDA_TARGET_ROOT/lib64/stubs/libdriver.so SONAME libdriver.so.7 DLOPEN_CALLBACK load_driver)")
        files["build/ymake_conf.py"] = lib.ymake_conf(extra="CUDA_TARGET_ROOT=$CUDA_RESOURCE_GLOBAL\n")
        files["mod/ya.make"] = files["mod/ya.make"].replace("END()", "PEERDIR(sdk)\nEND()")
        files["sdk/ya.make"] = "RESOURCES_LIBRARY()\nDECLARE_EXTERNAL_RESOURCE(CUDA sbr:1)\nEND()\n"
        graph = lib.make(files, "app", "--target-platform", "default-linux-x86_64")
        node = lib.only_node_by_kind(graph, "IL")
        self.assertEqual(node["cmds"][0]["cmd_args"], [
            "$(B)/contrib/tools/implib/implib", "--target", "x86_64",
            "--dlopen-callback", "load_driver", "--outdir", "$(B)/mod",
            "--library-load-name", "libdriver.so.7",
            "$(B)/resources/CUDA/lib64/stubs/libdriver.so",
        ])
        self.assertEqual(node["inputs"], ["$(B)/contrib/tools/implib/implib"])
        self.assertEqual(node["outputs"], ["$(B)/mod/libdriver.so.init.c", "$(B)/mod/libdriver.so.tramp.S"])
        for suffix in ("init.c", "tramp.S"):
            cc = lib.node_by_output(graph, f"$(B)/mod/libdriver.so.{suffix}.o")
            self.assertIn(node["uid"], cc["deps"])
            self.assertIn(f"$(B)/mod/libdriver.so.{suffix}", cc["inputs"])
        link = next(c["cmd_args"] for c in lib.node_by_output(graph, "$(B)/app/app")["cmds"]
                    if "$(S)/build/scripts/link_exe.py" in c["cmd_args"])
        self.assertEqual(link[link.index("--exclude-libs"):link.index("--exclude-libs") + 2], ["--exclude-libs", "driver"])

    def test_bundle_input_tracks_the_library_producer(self):
        files = self.fixture("GENERATE_IMPLIB(other libother.so FROM_BUNDLE)\nCOPY_FILE(input.so libother.so)")
        files["mod/input.so"] = ""
        graph = lib.make(files, "mod")
        node = lib.only_node_by_kind(graph, "IL")
        copied = lib.node_by_output(graph, "$(B)/mod/libother.so")
        self.assertIn(copied["uid"], node["deps"])
        self.assertIn("$(B)/mod/libother.so", node["inputs"])
        self.assertEqual(node["cmds"][0]["cmd_args"][-1], "$(B)/mod/libother.so")


if __name__ == "__main__":
    unittest.main(verbosity=2)
