from tst.make.lib import MakeTest


class MissingRootedRagelSourceTest(MakeTest):
    def test_missing_rooted_ragel_sources_allow_a_library_graph(self):
        self.write("ya.make", f"LIBRARY()\nSRCS(${{ARCADIA_ROOT}}/{self.project_name}/missing.rl6)\nEND()\n")
        graph = self.graph()
        library = self.node(graph, f"lib{self.project_name}.a")
        self.assertIn(library["uid"], graph["result"])
