from tst.make.lib import MakeTest, PROJECT


class FlatbufferKeywordSpacingTest(MakeTest):
    def test_include_keywords_accept_multiple_separating_spaces(self):
        self.write("ya.make", "LIBRARY()\nSRCS(answer.fbs dependency.fbs)\nEND()\n")
        self.write("answer.fbs", 'include   \t "dependency.fbs";\nnamespace e2e; table Answer { dependency:Dependency; }\n')
        self.write("dependency.fbs", "namespace e2e; table Dependency { value:int; }\n")
        generated = self.node(self.graph(), "answer.fbs.h")
        self.assertIn(f"$(S)/{PROJECT}/dependency.fbs", generated["inputs"])
