import unittest

from tst import lib
from tst.make.test_go_package import go_node
from tst.make.test_go_tests import go_module, go_test_world, resources_library, LLD_LDFLAGS


class GoSbomInputsTest(unittest.TestCase):
    def test_debug_program_consumes_transitive_components_without_embedding(self):
        self.check_consumes_components("GO_PROGRAM")

    def test_test_binary_consumes_transitive_components_without_embedding(self):
        self.check_consumes_components("GO_TEST")
        self.check_consumes_components("GO_TEST", "--release")

    def check_consumes_components(self, module, *extra_flags):
        files = go_test_world()
        files["build/internal/conf/sbom.conf"] = "\n"
        files["build/platform/python/ymake_python3/ya.make"] = (
            "RESOURCES_LIBRARY()\nTOOLCHAIN(python3)\nVERSION(3.12)\nEND()\n"
        )
        files["build/internal/platform/clang_toolchain_info/ya.make"] = "RESOURCES_LIBRARY()\nTOOLCHAIN(clang)\nVERSION(20)\nEND()\n"
        resources_library(files, "build/platform/lld", "LLD_ROOT", extra="TOOLCHAIN(lld)\nVERSION(20)\n" + LLD_LDFLAGS)
        go_module(files, "vendor/example.org/dependency", macros="LICENSE(MIT)\nVERSION(1)\n")
        go_module(files, "library/go/middle", 'import "example.org/dependency"\n')
        go_module(files, "cmd/main", 'import "a.yandex-team.ru/library/go/middle"\n', module=module)
        for autocheck in (False, True):
            with self.subTest(autocheck=autocheck):
                flags = ("-DAUTOCHECK=yes",) if autocheck else ()
                graph = lib.make(files, "cmd/main", "--target-platform", "default-linux-x86_64", *flags, *extra_flags)
                program = lib.node_by_output(graph, "$(B)/cmd/main/main")
                actual = {p for p in program["inputs"] if p.endswith(".component.sbom")}
                expected = set()
                if not autocheck:
                    middle = go_node(graph, "library/go/middle")
                    expected = {p for p in middle["inputs"] if p.endswith(".component.sbom")}
                    expected.update((
                        "$(B)/build/platform/lld/toolchain.component.sbom",
                        "$(B)/build/internal/platform/clang_toolchain_info/toolchain.component.sbom",
                    ))
                    self.assertIn("$(B)/vendor/example.org/dependency/dependency.GO.component.sbom", expected)
                    for path in expected:
                        component = lib.node_by_output(graph, path)
                        self.assertIn(component["uid"], program["deps"])
                self.assertEqual(actual, expected)
                self.assertNotIn("$(S)/build/internal/scripts/link_sbom.py", program["inputs"])
                self.assertFalse(any(".rosbomdata=" in arg for cmd in program["cmds"] for arg in cmd["cmd_args"]))
                self.assertFalse(any(arg.endswith("/link_sbom.py") for cmd in program["cmds"] for arg in cmd["cmd_args"]))


if __name__ == "__main__":
    unittest.main(verbosity=2)
