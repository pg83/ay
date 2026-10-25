import unittest

from tst import lib


class CiGroupTests(unittest.TestCase):
    def test_ci_group_collects_java_and_native_peer_closures(self):
        files = lib.java_tree(**{
            "build/platform/java/scriptgen/ya.make": lib.resource_lib("SCRIPTGEN", 7),
            "group/ya.make": "CI_GROUP()\nPEERDIR(app native)\nFILES(note.txt)\nEND()\n",
            "group/note.txt": "note",
            "app/ya.make": "JAVA_PROGRAM(app)\nPEERDIR(dep)\nEND()\n",
            "dep/ya.make": "JAVA_LIBRARY(dep)\nJAVA_SRCS(A.java)\nEND()\n",
            "dep/A.java": "class A {}\n",
        })
        lib.tool_program(files, "native", "native")
        lib.tool_program(files, "contrib/tools/ijar", "ijar")
        graph = lib.make(files, "group")
        group = lib.only_node_by_kind(graph, "CI")
        self.assertEqual(group["outputs"], ["$(B)/group/group.ci.pkg.fake"])
        self.assertEqual(group["cmds"][0]["cmd_args"][1:], [
            "$(S)/build/scripts/touch.py", "$(B)/group/group.ci.pkg.fake",
        ])
        self.assertEqual(group["kv"]["package"], "PACKAGE")
        self.assertIn("$(B)/group/note.txt", group["inputs"])
        for output in (
            "$(B)/app/app.run.cp.jar", "$(B)/app/app.jar",
            "$(B)/dep/dep.jar", "$(B)/dep/dep-interface.jar",
            "$(B)/native/native",
        ):
            node = lib.node_by_output(graph, output)
            self.assertIn(output, group["inputs"])
            self.assertIn(node["uid"], group["deps"])


if __name__ == "__main__":
    unittest.main()
