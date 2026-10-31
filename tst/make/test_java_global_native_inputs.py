from tst.make.lib import MakeTest, PROJECT


class JavaGlobalNativeInputsTest(MakeTest):
    def test_embedded_dll_keeps_its_global_native_sources(self):
        self.write("ya.make", f"JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nJAVA_SRCS(Answer.java)\nADD_DLLS_TO_JAR()\nPEERDIR({PROJECT}/native)\nEND()\n")
        self.write("Answer.java", "public class Answer { public native int answer(); }\n")
        self.write("native/ya.make", "DLL(answer)\nSRCS(GLOBAL value.cpp)\nEND()\n")
        self.write("native/value.cpp", 'extern "C" int value() { return 42; }\n')
        graph = self.graph()
        native = self.node(graph, "native/libanswer.so")
        compiled = self.node(graph, "native/value.cpp.pic.o")
        self.assertIn(compiled["uid"], native["deps"])
        self.assertIn(f"$(B)/{PROJECT}/native/value.cpp.pic.o", native["inputs"])
        self.assertIn(native["uid"], self.node(graph, "answer.jar")["deps"])
