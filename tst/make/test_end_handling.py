import json

from tst.make.lib import MakeTest, PROJECT


class EndHandlingTest(MakeTest):
    def graph_with_end_error(self):
        result = self.make("-k")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertRegex(result.stderr, r"END.*outside of a module")
        return json.loads(result.stdout.replace("$(BUILD_ROOT)", "$(B)"))

    def test_end_outside_module_preserves_valid_module(self):
        self.write("answer.cpp", "int answer() { return 42; }\n")
        self.write("finish.inc", "END()\n")
        module = "LIBRARY(answer)\nSRCS(answer.cpp)\nEND()\n"
        for description in (
            "END()\n" + module,
            module + "END()\n",
            "INCLUDE(finish.inc)\n" + module,
        ):
            with self.subTest(description=description):
                self.write("ya.make", description)
                self.assertNotEqual(self.make().returncode, 0)
                graph = self.graph_with_end_error()
                self.assertIn(self.node(graph, "libanswer.a")["uid"], graph["result"])

    def test_end_without_module_preserves_recurses(self):
        self.write("ya.make", "RECURSE(group)\n")
        self.write("group/ya.make", "IF(no)\nLIBRARY(unselected)\nENDIF()\nEND()\nRECURSE(child)\n")
        self.write("group/child/ya.make", "LIBRARY(answer)\nSRCS(answer.cpp)\nEND()\n")
        self.write("group/child/answer.cpp", "int answer() { return 42; }\n")
        graph = self.graph_with_end_error()
        self.assertEqual(graph["result"], [self.node(graph, "group/child/libanswer.a")["uid"]])

    def test_end_in_unselected_peer_module(self):
        self.write("ya.make", f"LIBRARY(answer)\nPEERDIR({PROJECT}/peer)\nSRCS(answer.cpp)\nEND()\nRECURSE(peer)\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        self.write("peer/child/ya.make", "LIBRARY(child)\nSRCS(child.cpp)\nEND()\n")
        self.write("peer/child/child.cpp", "int child() { return 42; }\n")
        for declaration in ("IF(no)\nLIBRARY(unselected)\nENDIF()", "UNKNOWN_E2E_MODULE(unselected)"):
            with self.subTest(declaration=declaration):
                self.write("peer/ya.make", declaration + "\nEND()\nRECURSE(child)\n")
                graph = self.graph_with_end_error()
                roots = [self.node(graph, output)["uid"] for output in ("libanswer.a", "peer/child/libchild.a")]
                self.assertCountEqual(graph["result"], roots)
                outputs = [output for node in graph["graph"] for output in node.get("outputs", [])]
                self.assertFalse(any("libunselected.a" in output for output in outputs))
