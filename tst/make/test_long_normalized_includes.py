from tst.make.lib import MakeTest


class LongNormalizedIncludesTest(MakeTest):
    def test_long_include_paths_with_redundant_segments_keep_the_header(self):
        directory = "/".join(f"segment{i:02d}" for i in range(35))
        self.write("ya.make", "LIBRARY()\nSRCS(answer.cpp)\nEND()\n")
        self.write(directory + "/answer.h", "#pragma once\n#define ANSWER 42\n")
        self.write("answer.cpp", f"#include <{self.project_name}/{directory}/./answer.h>\nint answer() {{ return ANSWER; }}\n")
        compiled = self.node(self.graph(), "answer.cpp.o")
        self.assertIn(f"$(S)/{self.project_name}/{directory}/answer.h", compiled["inputs"])
