from tst.make.lib import MakeTest, PROJECT


class FortranSourcePathsTest(MakeTest):
    def test_parent_source_paths_get_safe_object_output_names(self):
        self.write("ya.make", "RECURSE(lib)\n")
        self.write("lib/ya.make", "LIBRARY(answer)\nSRCS(../shared/answer.f)\nEND()\n")
        self.write("shared/answer.f", "      INTEGER FUNCTION ANSWER()\n      ANSWER = 42\n      END\n")
        node = self.node(self.graph(), "lib/__/shared/answer.f.o")
        self.assertIn(f"$(S)/{PROJECT}/shared/answer.f", node["inputs"])

    def test_internal_parent_segments_are_normalized_in_object_names(self):
        self.write("ya.make", "LIBRARY(answer)\nSRCS(nested/../answer.f)\nEND()\n")
        self.write("nested/.keep", "")
        self.write("answer.f", "      INTEGER FUNCTION ANSWER()\n      ANSWER = 42\n      END\n")
        node = self.node(self.graph(), "answer.f.o")
        self.assertIn(f"$(S)/{PROJECT}/answer.f", node["inputs"])
