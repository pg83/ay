from tst.make.lib import MakeTest


class RosImportedTypesTest(MakeTest):
    def test_go_and_pure_python_messages_use_their_own_source_inputs(self):
        self.write("Answer.msg", "Dep value\n")
        self.write("Dep.msg", "int32 number\n")
        for tag, settings in (("GO_ROS", ""), ("PY3_ROS", "ENABLE(ROS_PURE_PY)\n")):
            with self.subTest(tag=tag):
                self.write("ya.make", f"ROS_LIBRARY(e2e)\nONLY_TAGS({tag})\n{settings}ROS_SRCS(Answer.msg Dep.msg)\nEND()\n")
                graph = self.graph()
                source = f"$(S)/{self.project_name}/Answer.msg"
                generators = [node for node in graph["graph"] if source in node["inputs"]
                              and any(path.endswith(("/Answer.go", "/_Answer.py")) for path in node["outputs"])]
                self.assertEqual(len(generators), 1)
                self.assertNotIn(f"$(S)/{self.project_name}/Dep.msg", generators[0]["inputs"])
                self.assertTrue(any(path.endswith(("/Dep.go", "/_Dep.py"))
                                    for node in graph["graph"] for path in node["outputs"]))
