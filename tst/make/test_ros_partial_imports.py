from tst.make.lib import MakeTest


class RosPartialImportsTest(MakeTest):
    def test_python_message_and_service_packages_import_partial_modules(self):
        self.write("ya.make", "ROS_LIBRARY(e2e)\nONLY_TAGS(PY3_ROS)\nENABLE(ROS_PURE_PY)\nROS_SRCS(Answer.msg PARTIAL extra)\nEND()\n")
        self.write("Answer.msg", "int32 value\n")
        graph = self.graph()
        for kind in ("msg", "srv"):
            node = self.node(graph, kind + "/__init__.py")
            args = [arg for command in node["cmds"] for arg in command["cmd_args"]]
            self.assertIn(f"--from-import=extra.{kind}", args)
