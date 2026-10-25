from tst.make.lib import MakeTest, PROJECT


class RosVariantsTest(MakeTest):
    def test_languages_and_services(self):
        self.write("Answer.msg", "int32 value\n")
        self.write("Get.srv", "int32 question\n---\nint32 answer\n")
        for tag in ("CPP_ROS", "CPP_ROS PY3_CPP_ROS", "CPP_ROS PY3_CPP_ROS PY3_ROS", "GO_ROS"):
            with self.subTest(tag=tag):
                self.write("ya.make", f"ROS_LIBRARY(e2e)\nONLY_TAGS({tag})\nROS_SRCS(Answer.msg Get.srv)\nEND()\n")
                graph = self.graph()
                self.assertTrue(any(f"$(S)/{PROJECT}/Answer.msg" in node["inputs"] for node in graph["graph"]))

    def test_zero_copy(self):
        self.write("ya.make", "ROS_LIBRARY(e2e)\nONLY_TAGS(CPP_ROS)\nROS_SRCS(ZERO_COPY Answer.msg)\nEND()\n")
        self.write("Answer.msg", "int32 value\n")
        graph = self.graph()
        self.assertTrue(any(path.endswith("Answer_zero_copy.h") for node in graph["graph"] for path in node["outputs"]))

    def test_partial(self):
        self.write("ya.make", f"ROS_LIBRARY(e2e)\nONLY_TAGS(CPP_ROS)\nROS_DIR({PROJECT}/messages)\nROS_PARTIAL(e2e_part)\nROS_SRCS(messages/Answer.msg)\nEND()\n")
        self.write("messages/Answer.msg", "int32 value\n")
        self.graph()

    def test_pure_python(self):
        self.write("ya.make", "ROS_LIBRARY(e2e)\nONLY_TAGS(PY3_ROS)\nENABLE(ROS_PURE_PY)\nROS_SRCS(Answer.msg Get.srv)\nEND()\n")
        self.write("Answer.msg", "int32 value\n")
        self.write("Get.srv", "int32 question\n---\nint32 answer\n")
        graph = self.graph()
        self.assertTrue(any(path.endswith("msg/__init__.py") for node in graph["graph"] for path in node["outputs"]))
