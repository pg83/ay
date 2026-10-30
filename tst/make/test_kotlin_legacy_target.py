from tst.make.lib import MakeTest


class KotlinLegacyTargetTest(MakeTest):
    def test_unsupported_jdk8_is_rejected(self):
        self.write("ya.make", "JAVA_LIBRARY(answer)\nJDK_VERSION(8)\nWITH_KOTLIN()\nJAVA_SRCS(Answer.kt)\nEND()\n")
        self.write("Answer.kt", "class Answer(val value: Int = 42)\n")
        self.assertNotEqual(self.make().returncode, 0)
