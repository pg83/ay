from tst.make.lib import MakeTest


class IncludePathRootEscapeTest(MakeTest):
    def test_include_paths_cannot_walk_above_the_source_root(self):
        self.write("ya.make", "LIBRARY()\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", '#include "missing/../../../outside.h"\nint answer() { return 42; }\n')
        result = self.make()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("outside.h", result.stderr)
