from tst.make.lib import MakeTest, PROJECT


class ScannerPathsTest(MakeTest):
    def sysincl_mapping(self, paths):
        config = self.root / "build/sysincl/misc.yml"
        self.write_tree("build/sysincl/misc.yml", config.read_text() +
                        f'\n- source_filter: "^{PROJECT}/"\n  includes:\n    - e2e_alias.h:\n' +
                        "".join(f"      - {PROJECT}/{path}\n" for path in paths))

    def test_sysincl_repeated_targets_do_not_duplicate_inputs(self):
        self.sysincl_mapping(("first.h", "first.h", "second.h"))
        self.write("ya.make", "LIBRARY()\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "#include <e2e_alias.h>\nint answer() { return 42; }\n")
        for name in ("first.h", "second.h"):
            self.write(name, "#pragma once\n")
        inputs = self.node(self.graph(), "answer.cpp.o")["inputs"]
        for name in ("first.h", "second.h"):
            self.assertEqual(inputs.count(f"$(S)/{PROJECT}/{name}"), 1)

    def test_missing_sysincl_target_is_a_configuration_error(self):
        self.sysincl_mapping(("first.h", "missing.h", "second.h"))
        self.write("ya.make", "LIBRARY()\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "#include <e2e_alias.h>\nint answer() { return 42; }\n")
        for name in ("first.h", "second.h"):
            self.write(name, "#pragma once\n")
        self.assertNotEqual(self.make().returncode, 0)

    def test_sysincl_extends_an_existing_search_path_match(self):
        self.sysincl_mapping(("e2e_alias.h", "second.h"))
        self.write("ya.make", f"LIBRARY()\nADDINCL({PROJECT})\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "#include <e2e_alias.h>\nint answer() { return 42; }\n")
        for name in ("e2e_alias.h", "second.h"):
            self.write(name, "#pragma once\n")
        inputs = self.node(self.graph(), "answer.cpp.o")["inputs"]
        for name in ("e2e_alias.h", "second.h"):
            self.assertEqual(inputs.count(f"$(S)/{PROJECT}/{name}"), 1)

    def test_missing_sysincl_target_with_a_search_match_is_a_configuration_error(self):
        self.sysincl_mapping(("e2e_alias.h", "missing.h", "second.h"))
        self.write("ya.make", f"LIBRARY()\nADDINCL({PROJECT})\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "#include <e2e_alias.h>\nint answer() { return 42; }\n")
        for name in ("e2e_alias.h", "second.h"):
            self.write(name, "#pragma once\n")
        self.assertNotEqual(self.make().returncode, 0)

    def test_sysincl_without_existing_targets_is_a_configuration_error(self):
        self.sysincl_mapping(("missing.h", "other_missing.h"))
        self.write("ya.make", "LIBRARY()\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "#include <e2e_alias.h>\nint answer() { return 42; }\n")
        self.assertNotEqual(self.make().returncode, 0)

    def test_global_include_scope_requires_a_directory(self):
        self.write("ya.make", f"PROGRAM(answer)\nSRCS(main.cpp)\nPEERDIR({PROJECT}/lib)\nEND()\n")
        self.write("main.cpp", "int main() { return 0; }\n")
        self.write("lib/ya.make", 'LIBRARY()\nADDINCL(GLOBAL "")\nSRCS(value.cpp)\nEND()\n')
        self.write("lib/value.cpp", "int value() { return 42; }\n")
        self.assertNotEqual(self.make().returncode, 0)

    def test_empty_global_path_before_a_valid_peer_directory(self):
        self.write("ya.make", f"PROGRAM(answer)\nSRCS(main.cpp)\nPEERDIR({PROJECT}/lib)\nEND()\n")
        self.write("main.cpp", '#include <peer_value.h>\nint main() { return 0; }\n')
        self.write("lib/ya.make", f'LIBRARY()\nADDINCL(GLOBAL "" GLOBAL {PROJECT}/exported)\nSRCS(value.cpp)\nEND()\n')
        self.write("lib/value.cpp", "int value() { return 42; }\n")
        self.write("exported/peer_value.h", "#pragma once\n")
        inputs = self.node(self.graph(), "main.cpp.o")["inputs"]
        self.assertIn(f"$(S)/{PROJECT}/exported/peer_value.h", inputs)

    def one_level_fixture(self, global_peer):
        self.write("ya.make", f"PROGRAM(answer)\nSRCS(main.cpp)\nPEERDIR({PROJECT}/middle)\nEND()\n")
        self.write("main.cpp", "int main() { return 0; }\n")
        extra = f" {PROJECT}/global" if global_peer else ""
        self.write("middle/ya.make", f"LIBRARY()\nSRCS(middle.cpp)\nPEERDIR({PROJECT}/leaf{extra})\nEND()\n")
        self.write("middle/middle.cpp", '#include <peer_value.h>\nint middle() { return VALUE; }\n')
        self.write("leaf/ya.make", f"LIBRARY()\nADDINCL(ONE_LEVEL {PROJECT}/include)\nSRCS(leaf.cpp)\nEND()\n")
        self.write("leaf/leaf.cpp", "int leaf() { return 42; }\n")
        self.write("include/peer_value.h", "#define VALUE 42\n")
        if global_peer:
            self.write("global/ya.make", f"LIBRARY()\nADDINCL(GLOBAL {PROJECT}/include)\nSRCS(global.cpp)\nEND()\n")
            self.write("global/global.cpp", "int global_value() { return 42; }\n")
        graph = self.graph()
        expected = f"-I$(S)/{PROJECT}/include"
        for output, included in (("middle/middle.cpp.o", True), ("main.cpp.o", global_peer)):
            args = [arg.replace("$(SOURCE_ROOT)", "$(S)") for command in self.node(graph, output)["cmds"] for arg in command["cmd_args"]]
            self.assertEqual(expected in args, included, output)

    def test_one_level_include_directory_is_not_reexported(self):
        self.one_level_fixture(global_peer=False)

    def test_global_peer_overrides_one_level_include_limit(self):
        self.one_level_fixture(global_peer=True)

    def test_platform_header_search_after_an_empty_include_directory(self):
        for header in ("asm-generic/bitsperlong.h", "asm-generic/./bitsperlong.h"):
            for platform in ("", "NO_PLATFORM()\n"):
                with self.subTest(header=header, platform=platform):
                    self.write("answer.cpp", f"#include <{header}>\nint answer() {{ return 42; }}\n")
                    self.write("ya.make", 'LIBRARY()\nNO_RUNTIME()\n' + platform + 'ADDINCL("")\nSRCS(answer.cpp)\nEND()\n')
                    inputs = self.node(self.graph(), "answer.cpp.o")["inputs"]
                    self.assertTrue(any(path.endswith("/asm-generic/bitsperlong.h") for path in inputs), inputs)

    def test_unresolved_include_exhausts_unindexed_search_directories(self):
        self.write("ya.make", 'LIBRARY()\nADDINCL("")\nSRCS(answer.cpp)\nEND()\n')
        self.write("answer.cpp", "#include <e2e_missing_header.h>\nint answer() { return 42; }\n")
        result = self.make()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("e2e_missing_header.h", result.stderr)

    def test_source_root_as_local_include_directory(self):
        self.write("answer.cpp", f'#include <{PROJECT}/headers/value.h>\nint answer() {{ return VALUE; }}\n')
        self.write("headers/value.h", "#define VALUE 42\n")
        for directory in ('${ARCADIA_ROOT}/', '""'):
            with self.subTest(directory=directory):
                self.write("ya.make", f"LIBRARY()\nADDINCL({directory})\nSRCS(answer.cpp)\nEND()\n")
                self.assertIn(f"$(S)/{PROJECT}/headers/value.h", self.node(self.graph(), "answer.cpp.o")["inputs"])

    def test_source_root_exported_by_a_peer(self):
        self.write("ya.make", f"PROGRAM(answer)\nSRCS(main.cpp)\nPEERDIR({PROJECT}/lib)\nEND()\n")
        self.write("main.cpp", f'#include <{PROJECT}/headers/value.h>\nint main() {{ return VALUE; }}\n')
        self.write("lib/value.cpp", "int value() { return 42; }\n")
        self.write("headers/value.h", "#define VALUE 42\n")
        self.write("lib/ya.make", "LIBRARY()\nADDINCL(GLOBAL ${ARCADIA_ROOT}/)\nSRCS(value.cpp)\nEND()\n")
        self.assertIn(f"$(S)/{PROJECT}/headers/value.h", self.node(self.graph(), "main.cpp.o")["inputs"])

    def test_build_root_fallback_with_source_root_include_directory(self):
        self.write("answer.cpp", f'#include <{PROJECT}/generated/value.h>\nint answer() {{ return VALUE; }}\n')
        self.write("value.h", "#define VALUE 42\n")
        for directory in ('${ARCADIA_ROOT}/', '""'):
            with self.subTest(directory=directory):
                self.write("ya.make", f"LIBRARY()\nADDINCL({directory})\nCOPY_FILE_WITH_CONTEXT(value.h generated/value.h)\nSRCS(answer.cpp)\nEND()\n")
                graph = self.graph()
                compiled = self.node(graph, "answer.cpp.o")
                generated = self.node(graph, "generated/value.h")
                self.assertIn(f"$(B)/{PROJECT}/generated/value.h", compiled["inputs"])
                self.assertIn(generated["uid"], compiled["deps"])
