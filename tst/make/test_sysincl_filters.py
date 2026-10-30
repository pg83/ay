import json

from tst.make.lib import MakeTest, PROJECT


class SysinclFiltersTest(MakeTest):
    def setUp(self):
        super().setUp()
        self.config = (self.root / "build/sysincl/misc.yml").read_text()
        self.write("ya.make", "LIBRARY()\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "#include <e2e_alias.h>\nint answer() { return VALUE; }\n")
        self.write("value.h", "#define VALUE 42\n")

    def mapping(self, pattern, insensitive=False):
        key = "E2E_ALIAS.H" if insensitive else "e2e_alias.h"
        sensitive = "false" if insensitive else "true"
        self.write_tree("build/sysincl/misc.yml", self.config + "\n- source_filter: " + json.dumps(pattern, ensure_ascii=False) + f"\n  case_sensitive: {sensitive}\n  includes:\n    - {key}:\n      - {PROJECT}/value.h\n")

    def assert_mapped(self, pattern, insensitive=False):
        self.mapping(pattern, insensitive)
        self.assertIn(f"$(S)/{PROJECT}/value.h", self.node(self.graph(), "answer.cpp.o")["inputs"])

    def test_case_insensitive_include_keys_match_lowercase_spelling(self):
        self.assert_mapped("^" + PROJECT + "/", insensitive=True)

    def test_captures_and_empty_alternatives_match_source_paths(self):
        for pattern in (f"^({PROJECT}/)(ans(wer))[.]cpp$", f"^(unused|){PROJECT}/answer[.]cpp$", f"^(first|second|{PROJECT})/answer[.]cpp$"):
            with self.subTest(pattern=pattern):
                self.assert_mapped(pattern)

    def test_large_literal_alternative_sets_keep_regex_matching(self):
        choices = "".join("(" + chr(ord("a") + i) + "|)" for i in range(7))
        alternatives = [chr(0x100 + i) + str(i) for i in range(65)] + [PROJECT]
        patterns = ("^" + choices + PROJECT + "/answer[.]cpp$", "^(" + choices + ")" + PROJECT + "/answer[.]cpp$", "^(" + "|".join(alternatives) + ")/answer[.]cpp$")
        for pattern in patterns:
            with self.subTest(pattern=pattern):
                self.assert_mapped(pattern)

    def test_contains_pattern_without_a_trailing_wildcard(self):
        self.assert_mapped(".*" + PROJECT)

    def test_case_folded_regex_literals_match_source_paths(self):
        self.assert_mapped("(?i)^" + PROJECT.upper() + "/ANSWER[.]CPP$")

    def test_valid_negative_lookahead_combinations_apply_mappings(self):
        patterns = (f"^{PROJECT}/(?!(excluded|unused))answer[.]cpp$", f"^(?:{PROJECT}/)(?!excluded)answer[.]cpp$", f"^(?!{PROJECT}/excluded)(?!{PROJECT}/ignored){PROJECT}/")
        for pattern in patterns:
            with self.subTest(pattern=pattern):
                self.assert_mapped(pattern)

    def test_invalid_source_filter_regex_is_a_configuration_error(self):
        self.mapping("[")
        self.assertNotEqual(self.make().returncode, 0)
