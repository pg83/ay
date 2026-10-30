from tst.make.lib import MakeTest


class ManyIncludeDirectivesTest(MakeTest):
    def test_more_than_sixty_five_thousand_includes_keep_the_header_dependency(self):
        self.write("ya.make", "LIBRARY()\nSRCS(answer.cpp)\nEND()\n")
        self.write("leaf.h", "#pragma once\n")
        self.write("answer.cpp", '#include "leaf.h"\n' * 65_537 + "int answer() { return 42; }\n")
        node = self.node(self.graph(), "answer.cpp.o")
        self.assertIn(f"$(S)/{self.project_name}/leaf.h", node["inputs"])
