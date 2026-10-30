from tst.make.lib import MakeTest


class MissingRosTypeInputsTest(MakeTest):
    def test_cpp_ros_graph_allows_unresolved_qualified_type_definitions(self):
        self.write("ya.make", "ROS_LIBRARY(e2e)\nONLY_TAGS(CPP_ROS)\nROS_SRCS(Answer.msg)\nEND()\n")
        self.write("Answer.msg", "e2e_missing/Value value\n")
        generated = self.node(self.graph(), "Answer.h")
        self.assertIn(f"$(S)/{self.project_name}/Answer.msg", generated["inputs"])
