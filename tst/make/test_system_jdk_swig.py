from tst.make.lib import MakeTest


class SystemJdkSwigTest(MakeTest):
    def test_java_swig_generation_accepts_a_system_jdk(self):
        self.write("ya.make", "DLL_JAVA(answer)\nJDK_VERSION(17)\nSET(USE_SYSTEM_JDK yes)\nSRCS(answer.swg)\nEND()\n")
        self.write("answer.swg", '%module answer\n%inline %{ int answer() { return 42; } %}\n')
        self.node(self.graph(), "answer.swg.jsrc")
