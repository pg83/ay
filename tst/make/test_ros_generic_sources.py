from tst.make.lib import MakeTest


class RosGenericSourcesTest(MakeTest):
    def test_generic_ros_message_sources_require_a_ros_module(self):
        self.write("Answer.msg", "int32 value\n")
        self.write("ya.make", "ROS_LIBRARY(e2e)\nONLY_TAGS(CPP_ROS)\nSRCS(Answer.msg)\nEND()\n")
        graph = self.graph()
        self.assertTrue(any(f"$(S)/{self.project_name}/Answer.msg" in node["inputs"] and any(path.endswith("/Answer.h") for path in node["outputs"]) for node in graph["graph"]))
        self.write("ya.make", "LIBRARY()\nSRCS(answer.cpp Answer.msg)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        self.assertNotEqual(self.make().returncode, 0)
