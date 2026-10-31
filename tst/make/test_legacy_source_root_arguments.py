from tst.make.lib import MakeTest


class LegacySourceRootArgumentsTest(MakeTest):
    def test_shorthand_source_root_paths_select_declared_files(self):
        self.write("ya.make", f"LIBRARY()\nSRCS($S/{self.project_name}/answer.cpp)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        node = self.node(self.graph(), "answer.cpp.o")
        self.assertIn(f"$(S)/{self.project_name}/answer.cpp", node["inputs"])
