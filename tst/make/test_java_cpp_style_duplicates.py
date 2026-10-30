from tst.make.lib import MakeTest, PROJECT


class JavaCppStyleDuplicatesTest(MakeTest):
    def test_repeated_cpp_style_declarations_create_one_header_check(self):
        self.write("ya.make", "JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nJAVA_SRCS(Answer.java)\nSTYLE_CPP()\nSTYLE_CPP()\nEND()\n")
        self.write("Answer.java", "public class Answer {}\n")
        self.write("answer.h", "#pragma once\n")
        checked = self.node(self.graph("-t"), "test-results/clang_format/meta.json")
        self.assertIn(f"$(S)/{PROJECT}/answer.h", checked["inputs"])
