from tst.make.lib import MakeTest


class RosMacroContextTest(MakeTest):
    def test_ros_directory_macro_is_ignored_in_an_ordinary_library(self):
        self.write("ya.make", "LIBRARY()\nROS_DIR(messages)\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        node = self.node(self.graph(), "answer.cpp.o")
        self.assertIn(f"$(S)/{self.project_name}/answer.cpp", node["inputs"])
