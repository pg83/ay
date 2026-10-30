from tst.make.lib import MakeTest, PROJECT


class GlobTest(MakeTest):
    def test_large_character_classes_and_alternatives(self):
        names = ("ab.txt", "Z9.txt", "a-.txt", "only.txt", "skip.cpp", "prefix42.txt")
        for name in names:
            self.write("data/" + name, name + "\n")
        patterns = (
            ("*[a-zA-Z0-9_][a-zA-Z0-9_].txt", ("ab.txt", "Z9.txt", "only.txt", "prefix42.txt")),
            ("*[!-~].txt", ("ab.txt", "Z9.txt", "a-.txt", "only.txt", "prefix42.txt")),
            ("*(ab|Z9|only).txt", ("ab.txt", "Z9.txt", "only.txt")),
            ("*(a|b|c|d|e|f|g|h|i)(a|b|c|d|e|f|g|h|i).txt", ("ab.txt",)),
        )
        self.write("generate.py", "print('collected')\n")
        for pattern, expected in patterns:
            with self.subTest(pattern=pattern):
                self.write("ya.make", f'LIBRARY()\nDECLARE_IN_DIRS(DATA "{pattern}" DIRS data)\nRUN_PYTHON3(generate.py IN ${{DATA_FILES}} STDOUT result.txt)\nRESOURCE(result.txt /result)\nEND()\n')
                inputs = self.node(self.graph(), "result.txt")["inputs"]
                actual = {name for name in names if f"$(S)/{PROJECT}/data/{name}" in inputs}
                self.assertEqual(actual, set(expected))

    def test_headers_suffix_alternatives(self):
        selected = tuple(f"include/answer.{ext}" for ext in ("h", "H", "hh", "hpp", "hxx", "ipp"))
        for name in (*selected, "include/skip.h", "include/answer.txt", "include/sub/nested.h"):
            self.write(name, "#pragma once\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        self.write("ya.make", 'LIBRARY()\nSRCS(answer.cpp)\nHEADERS(include EXCLUDE include/skip.h)\nSTYLE_CPP()\nEND()\n')
        inputs = {path for node in self.graph("-t")["graph"] for path in node.get("inputs", [])}
        for name in selected:
            self.assertIn(f"$(S)/{PROJECT}/{name}", inputs)
        for name in ("include/skip.h", "include/answer.txt", "include/sub/nested.h"):
            self.assertNotIn(f"$(S)/{PROJECT}/{name}", inputs)

    def test_regex_repetition_and_unicode_names(self):
        names = ("ab.txt", "abab.txt", "AB.TXT", "éx.txt", "éfile.txt", "�.txt", "skip.cpp")
        for name in names:
            self.write("data/" + name, name + "\n")
        patterns = (
            ("*(ab)+.txt", ("ab.txt", "abab.txt")),
            ("*?x.txt", ("éx.txt",)),
            ("*�.txt", ("�.txt",)),
            ("*(a|ab)*.txt", ("ab.txt", "abab.txt")),
        )
        self.write("generate.py", "print('collected')\n")
        for pattern, expected in patterns:
            with self.subTest(pattern=pattern):
                self.write("ya.make", f'LIBRARY()\nDECLARE_IN_DIRS(DATA "{pattern}" DIRS data)\nRUN_PYTHON3(generate.py IN ${{DATA_FILES}} STDOUT result.txt)\nRESOURCE(result.txt /result)\nEND()\n')
                inputs = self.node(self.graph(), "result.txt")["inputs"]
                actual = {name for name in names if f"$(S)/{PROJECT}/data/{name}" in inputs}
                self.assertEqual(actual, set(expected))

    def test_hex_escape_in_a_negated_character_class(self):
        # Keep the source spelling: upstream's late glob treats this escape
        # differently from the regular expression used by ay.
        names = ("ab.txt", "éx.txt", "éfile.txt")
        for name in names:
            self.write("data/" + name, name + "\n")
        self.write("generate.py", "print('collected')\n")
        self.write("ya.make", 'LIBRARY()\nDECLARE_IN_DIRS(DATA "*[^\\x2f].txt" DIRS data)\nRUN_PYTHON3(generate.py IN ${DATA_FILES} STDOUT result.txt)\nRESOURCE(result.txt /result)\nEND()\n')
        inputs = self.node(self.graph(), "result.txt")["inputs"]
        actual = {name for name in names if f"$(S)/{PROJECT}/data/{name}" in inputs}
        self.assertEqual(actual, {"ab.txt", "éfile.txt"})

    def test_escaped_letters_in_a_character_class(self):
        names = ("passes.txt", "voiceS.txt", "value.txt")
        for name in names:
            self.write("data/" + name, name + "\n")
        self.write("generate.py", "print('collected')\n")
        self.write("ya.make", 'LIBRARY()\nDECLARE_IN_DIRS(DATA "*[\\s\\S].txt" DIRS data)\nRUN_PYTHON3(generate.py IN ${DATA_FILES} STDOUT result.txt)\nRESOURCE(result.txt /result)\nEND()\n')
        inputs = self.node(self.graph(), "result.txt")["inputs"]
        actual = {name for name in names if f"$(S)/{PROJECT}/data/{name}" in inputs}
        self.assertEqual(actual, {"passes.txt", "voiceS.txt"})

    def test_more_than_sixty_four_alternatives(self):
        alternatives = ["ab", *(f"variant{index}p{index}x" for index in range(64))]
        pattern = "*(" + "|".join(alternatives) + ").txt"
        self.write("data/ab.txt", "42\n")
        self.write("data/skip.txt", "17\n")
        self.write("generate.py", "print('collected')\n")
        self.write("ya.make", f'LIBRARY()\nDECLARE_IN_DIRS(DATA "{pattern}" DIRS data)\nRUN_PYTHON3(generate.py IN ${{DATA_FILES}} STDOUT result.txt)\nRESOURCE(result.txt /result)\nEND()\n')
        inputs = self.node(self.graph(), "result.txt")["inputs"]
        self.assertIn(f"$(S)/{PROJECT}/data/ab.txt", inputs)
        self.assertNotIn(f"$(S)/{PROJECT}/data/skip.txt", inputs)

    def test_invalid_pattern_is_a_configuration_error(self):
        self.write("ya.make", 'LIBRARY()\nDECLARE_IN_DIRS(DATA "*[unclosed.txt" DIRS data)\nEND()\n')
        self.write("data/value.txt", "42\n")
        self.assertNotEqual(self.make().returncode, 0)

    def test_ascii_case_pair_in_a_character_class(self):
        self.write("data/a.txt", "lowercase\n")
        self.write("data/A.txt", "uppercase\n")
        self.write("data/skip.txt", "excluded\n")
        self.write("generate.py", "print('collected')\n")
        self.write("ya.make", 'LIBRARY()\nDECLARE_IN_DIRS(DATA "*[aA].txt" DIRS data)\nRUN_PYTHON3(generate.py IN ${DATA_FILES} STDOUT result.txt)\nRESOURCE(result.txt /result)\nEND()\n')
        inputs = self.node(self.graph(), "result.txt")["inputs"]
        self.assertIn(f"$(S)/{PROJECT}/data/a.txt", inputs)
        self.assertIn(f"$(S)/{PROJECT}/data/A.txt", inputs)
        self.assertNotIn(f"$(S)/{PROJECT}/data/skip.txt", inputs)

    def test_recursive_exclusion_matches_nested_and_root_files(self):
        config = self.root / "build/ymake.core.conf"
        self.write_tree("build/ymake.core.conf", config.read_text() + '''
macro E2E_RECURSIVE_FILES() {
    _GLOB(E2E_FILES **/*.txt EXCLUDE **/skip.txt)
    RUN_PYTHON3(generate.py IN $E2E_FILES STDOUT result.txt)
    RESOURCE(result.txt /result)
}
''')
        self.write("ya.make", "LIBRARY()\nE2E_RECURSIVE_FILES()\nEND()\n")
        self.write("generate.py", "print('collected')\n")
        for name in ("root.txt", "skip.txt", "data/keep.txt", "data/skip.txt", "data/nested/keep.txt", "data/nested/skip.txt"):
            self.write(name, name + "\n")
        inputs = self.node(self.graph(), "result.txt")["inputs"]
        for name in ("root.txt", "data/keep.txt", "data/nested/keep.txt"):
            self.assertIn(f"$(S)/{PROJECT}/{name}", inputs)
        for name in ("skip.txt", "data/skip.txt", "data/nested/skip.txt"):
            self.assertNotIn(f"$(S)/{PROJECT}/{name}", inputs)

    def test_epoch_wraparound_preserves_glob_matches(self):
        self.write("ya.make", 'LIBRARY()\nDECLARE_IN_DIRS(DATA "*[!-~].txt" DIRS data)\nRUN_PYTHON3(generate.py IN ${DATA_FILES} STDOUT result.txt)\nRESOURCE(result.txt /result)\nEND()\n')
        self.write("generate.py", "print('collected')\n")
        for name in ("first.txt", "second.txt", "skip.cpp"):
            self.write("data/" + name, name + "\n")
        graph = self.graph(env={"AY_CHAOS": "epoch-start=4294967295"})
        inputs = self.node(graph, "result.txt")["inputs"]
        self.assertIn(f"$(S)/{PROJECT}/data/first.txt", inputs)
        self.assertIn(f"$(S)/{PROJECT}/data/second.txt", inputs)
        self.assertNotIn(f"$(S)/{PROJECT}/data/skip.cpp", inputs)
