from tst.make.lib import MakeTest, PROJECT


class LongIncludePathsTest(MakeTest):
    def test_redundant_segments_in_deep_and_long_include_paths(self):
        self.write("ya.make", "LIBRARY()\nSRCS(answer.cpp)\nEND()\n")
        for segment, depth in (("a", 70), ("long_segment", 30)):
            with self.subTest(segment=segment, depth=depth):
                directory = "/".join([segment] * depth)
                self.write(directory + "/value.h", "#define VALUE 42\n")
                self.write("answer.cpp", f'#include "./{directory}//discard/.././value.h"\nint answer() {{ return VALUE; }}\n')
                self.assertIn(f"$(S)/{PROJECT}/{directory}/value.h", self.node(self.graph(), "answer.cpp.o")["inputs"])
