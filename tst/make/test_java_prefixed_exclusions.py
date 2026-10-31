from tst.make.lib import MakeTest, PROJECT


class JavaPrefixedExclusionsTest(MakeTest):
    def test_double_slash_exclusions_remove_matching_prefixed_jars(self):
        self.write("ya.make", f"JAVA_PROGRAM(answer)\nJDK_VERSION(17)\nJAVA_SRCS(Main.java)\nPEERDIR({PROJECT}/middle)\nEXCLUDE({PROJECT}/leaf/nested//)\nEND()\n")
        self.write("Main.java", "public class Main {}\n")
        self.write("middle/ya.make", f"JAVA_LIBRARY(middle)\nJDK_VERSION(17)\nJAVA_SRCS(Middle.java)\nPEERDIR({PROJECT}/leaf)\nEND()\n")
        self.write("middle/Middle.java", "public class Middle {}\n")
        self.write("leaf/ya.make", "JAVA_LIBRARY(answer PREFIX nested//)\nJDK_VERSION(17)\nJAVA_SRCS(Answer.java)\nEND()\n")
        self.write("leaf/Answer.java", "public class Answer {}\n")
        inputs = self.node(self.graph(), "answer.run.cp.jar")["inputs"]
        self.assertIn(f"$(B)/{PROJECT}/middle/middle.jar", inputs)
        self.assertFalse(any(path.startswith(f"$(B)/{PROJECT}/leaf/") for path in inputs))
