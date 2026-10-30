from tst.make.lib import MakeTest


class GoForwardGeneratorOrderTest(MakeTest):
    def test_forward_dependencies_preserve_both_automatic_go_sources(self):
        self.write("ya.make", "GO_LIBRARY()\nRUN_PYTHON3(first.py IN intermediate.txt OUT first.go)\nRUN_PYTHON3(second.py OUT intermediate.txt second.go)\nEND()\n")
        self.write("first.py", "from pathlib import Path\nPath('first.go').write_text('package answer\\nconst First = 1\\n')\n")
        self.write("second.py", "from pathlib import Path\nPath('intermediate.txt').write_text('42')\nPath('second.go').write_text('package answer\\nconst Second = 2\\n')\n")
        graph = self.graph()
        first = self.node(graph, "first.go")
        second = self.node(graph, "second.go")
        self.assertIn(second["uid"], first["deps"])
        archive = self.node(graph, self.project_name + ".a")
        for name in ("first.go", "second.go"):
            self.assertIn(f"$(B)/{self.project_name}/{name}", archive["inputs"])
        for node in (first, second):
            self.assertIn(node["uid"], archive["deps"])
