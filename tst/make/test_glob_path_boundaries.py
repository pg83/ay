from tst.make.lib import MakeTest, PROJECT


class GlobPathBoundariesTest(MakeTest):
    def test_exclusions_match_one_character_and_repeated_separators(self):
        core = self.root / "build/ymake.core.conf"
        self.write_tree("build/ymake.core.conf", core.read_text() + '''
macro E2E_BOUNDARY_GLOB() {
    _GLOB(E2E_FILES **/*.txt EXCLUDE data/skip?.txt data//ignored.txt)
    RUN_PYTHON3(generate.py IN $E2E_FILES STDOUT result.txt)
    RESOURCE(result.txt /result)
}
''')
        self.write("ya.make", "LIBRARY()\nE2E_BOUNDARY_GLOB()\nEND()\n")
        self.write("generate.py", "print('collected')\n")
        for name in ("root.txt", "data/keep.txt", "data/skip1.txt", "data/ignored.txt"):
            self.write(name, "42\n")
        inputs = self.node(self.graph(), "result.txt")["inputs"]
        for name in ("root.txt", "data/keep.txt"):
            self.assertIn(f"$(S)/{PROJECT}/{name}", inputs)
        for name in ("data/skip1.txt", "data/ignored.txt"):
            self.assertNotIn(f"$(S)/{PROJECT}/{name}", inputs)

    def test_unknown_glob_restrictions_are_rejected(self):
        core = self.root / "build/ymake.core.conf"
        self.write_tree("build/ymake.core.conf", core.read_text() + '\nmacro E2E_INVALID_GLOB() {\n    _GLOB(E2E_FILES *.txt RESTRICTIONS UNKNOWN_LIMIT 1)\n}\n')
        self.write("ya.make", "LIBRARY()\nE2E_INVALID_GLOB()\nEND()\n")
        self.write("value.txt", "42\n")
        self.assertNotEqual(self.make().returncode, 0)
