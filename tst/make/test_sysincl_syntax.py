from tst.make.lib import MakeTest, PROJECT


class SysinclSyntaxTest(MakeTest):
    def test_unknown_record_keys_are_configuration_errors(self):
        config = self.root / "build/sysincl/misc.yml"
        self.write_tree("build/sysincl/misc.yml", config.read_text() + f'\n- source_filter: "^{PROJECT}/"\n  e2e_unknown_key: value\n  includes: []\n')
        self.write("ya.make", "LIBRARY()\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        result = self.make()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("e2e_unknown_key", result.stderr)

    def test_malformed_top_level_records_are_rejected(self):
        self.write("ya.make", "LIBRARY()\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        for content in ("not-a-mapping\n", "includes: [e2e_alias.h]\n", "  - e2e_alias.h\n", "  includes: []\n"):
            with self.subTest(content=content):
                self.write_tree("build/sysincl/misc.yml", content)
                self.assertNotEqual(self.make().returncode, 0)

    def fixture(self, suffix, header="e2e_alias.h"):
        config = self.root / "build/sysincl/misc.yml"
        self.write_tree("build/sysincl/misc.yml", config.read_text() + "\n" + suffix)
        self.write("ya.make", "LIBRARY()\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", f"#include <{header}>\nint answer() {{ return VALUE; }}\n")
        self.write("value.h", "#define VALUE 42\n")

    def test_quoted_include_keys_can_contain_an_escaped_quote(self):
        self.fixture(f'- source_filter: "^{PROJECT}/"\n  includes:\n    - "e2e_\\\"quoted.h":\n      - {PROJECT}/value.h\n', 'e2e_"quoted.h')
        self.assertIn(f"$(S)/{PROJECT}/value.h", self.node(self.graph(), "answer.cpp.o")["inputs"])

    def test_final_mapping_without_a_newline_and_with_trailing_spaces(self):
        self.fixture(f'- source_filter: "^{PROJECT}/"  \r\n  includes:  \r\n    - e2e_alias.h:  \r\n      - {PROJECT}/value.h   \r')
        self.assertIn(f"$(S)/{PROJECT}/value.h", self.node(self.graph(), "answer.cpp.o")["inputs"])

    def test_tab_indentation_is_rejected(self):
        self.fixture(f'- source_filter: "^{PROJECT}/"\n\tincludes:\n    - e2e_alias.h:\n      - {PROJECT}/value.h\n')
        self.assertNotEqual(self.make().returncode, 0)

    def test_empty_sequence_items_are_rejected(self):
        self.fixture('-\n')
        self.assertNotEqual(self.make().returncode, 0)

    def test_invalid_quoted_scalars_are_rejected(self):
        config = self.root / "build/sysincl/misc.yml"
        original = config.read_text()
        self.write("ya.make", "LIBRARY()\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        for value in ('"unterminated', '"dangling\\"', '"unsupported\\q"'):
            with self.subTest(value=value):
                self.write_tree("build/sysincl/misc.yml", original + f'\n- source_filter: {value}\n  includes: []\n')
                self.assertNotEqual(self.make().returncode, 0)
