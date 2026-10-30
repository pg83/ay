from tst.make.lib import MakeTest


class RootedRecursiveGlobPathsTest(MakeTest):
    def test_rooted_recursive_globs_preserve_files_across_parent_segments(self):
        core = self.root / "build/ymake.core.conf"
        declaration = f'''
macro E2E_ROOTED_GLOB() {{
    _GLOB(E2E_FILES ${{ARCADIA_ROOT}}/{self.project_name}/data/../data/**/*.txt)
    RUN_PYTHON3(generate.py IN $E2E_FILES STDOUT result.txt)
    RESOURCE(result.txt /result)
}}
'''
        self.write_tree("build/ymake.core.conf", core.read_text() + declaration)
        self.write("ya.make", "LIBRARY()\nE2E_ROOTED_GLOB()\nEND()\n")
        self.write("generate.py", "print('collected')\n")
        for name in ("data/first.txt", "data/nested/second.txt", "data/nested/skip.cpp"):
            self.write(name, "42\n")
        inputs = self.node(self.graph(), "result.txt")["inputs"]
        prefix = f"$(S)/{self.project_name}/data/"
        self.assertEqual({path for path in inputs if path.startswith(prefix)},
                         {prefix + "first.txt", prefix + "nested/second.txt"})
