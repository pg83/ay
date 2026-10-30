from tst.make.lib import MakeTest


class RosSearchPathsTest(MakeTest):
    def test_explicit_ros_directory_preserves_go_message_type_inputs(self):
        self.write("ya.make", f"ROS_LIBRARY(e2e)\nONLY_TAGS(GO_ROS)\nROS_DIR({self.project_name})\nROS_SRCS(Answer.msg Dep.msg)\nEND()\n")
        self.write("Answer.msg", "Dep value\n")
        self.write("Dep.msg", "int32 number\n")
        generated = self.node(self.graph(), "Answer.go")
        self.assertIn(f"$(S)/{self.project_name}/Answer.msg", generated["inputs"])
        self.assertIn(f"$(S)/{self.project_name}/Dep.msg", generated["inputs"])
