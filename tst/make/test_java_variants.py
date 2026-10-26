import zipfile

from tst.make.lib import MakeTest, PROJECT


class JavaVariantsTest(MakeTest):
    def test_uberjar_unpack_declares_native_libraries(self):
        self.write("ya.make", f"JAVA_PROGRAM(answer)\nJDK_VERSION(17)\nJAVA_SRCS(Main.java)\nUBERJAR()\nADD_DLLS_TO_JAR()\nPEERDIR({PROJECT}/native)\nEND()\n")
        self.write("Main.java", "public class Main { public static void main(String[] args) {} }\n")
        self.write("native/ya.make", "DLL(value)\nSRCS(value.cpp)\nEND()\n")
        self.write("native/value.cpp", 'extern "C" int value() { return 42; }\n')
        node = self.node(self.graph(), "answer.run.cp.jar")
        self.assertIn(f"$(B)/{PROJECT}/answer/libvalue.so", node["outputs"])

    def test_uberjar(self):
        self.write("ya.make", "JAVA_PROGRAM(answer)\nJDK_VERSION(17)\nJAVA_SRCS(Main.java)\nUBERJAR()\nUBERJAR_MANIFEST_TRANSFORMER_MAIN(e2e.Main)\nUBERJAR_MANIFEST_TRANSFORMER_ATTRIBUTE(E2E 42)\nUBERJAR_SERVICES_RESOURCE_TRANSFORMER()\nEND()\n")
        self.write("Main.java", "package e2e; public class Main { public static void main(String[] args) {} }\n")
        self.node(self.graph(), "answer.jar")

    def test_kotlin(self):
        self.write("ya.make", "JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nWITH_KOTLIN()\nJAVA_SRCS(Answer.kt)\nKOTLINC_FLAGS(-Xjsr305=strict)\nEND()\n")
        self.write("Answer.kt", "package e2e\nfun answer(): Int = 42\n")
        graph = self.graph("-t")
        self.node(graph, "answer.jar")
        self.assertTrue(any(f"$(S)/{PROJECT}/Answer.kt" in node["inputs"] for node in graph["graph"]))

    def test_local_java_archives(self):
        self.write("ya.make", "JAVA_CONTRIB(answer)\nLOCAL_JAR(answer.jar)\nLOCAL_SOURCES_JAR(sources.jar)\nEND()\n")
        with zipfile.ZipFile(self.project / "answer.jar", "w") as archive:
            archive.writestr("META-INF/MANIFEST.MF", "Manifest-Version: 1.0\n")
        with zipfile.ZipFile(self.project / "sources.jar", "w") as archive:
            archive.writestr("e2e/Value.java", "package e2e; public class Value {}\n")
        graph = self.graph()
        self.node(graph, "answer.jar")
        self.assertTrue(any(f"$(S)/{PROJECT}/answer.jar" in node["inputs"] for node in graph["graph"]))

    def test_native_library_in_jar(self):
        self.write("ya.make", f"JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nJAVA_SRCS(Answer.java)\nADD_DLLS_TO_JAR()\nPEERDIR({PROJECT}/native)\nEND()\n")
        self.write("Answer.java", "package e2e; public class Answer { public native int answer(); }\n")
        self.write("native/ya.make", "DLL(answer)\nSRCS(answer.cpp)\nEND()\n")
        self.write("native/answer.cpp", 'extern "C" int answer() { return 42; }\n')
        graph = self.graph()
        self.node(graph, "native/libanswer.so")
        self.node(graph, "answer.jar")

    def test_junit_system_properties(self):
        self.write("ya.make", "JUNIT5(answer)\nJDK_VERSION(17)\nJAVA_SRCS(AnswerTest.java)\nSYSTEM_PROPERTIES(e2e.answer 42)\nJVM_ARGS(-Xmx128m)\nFORK_SUBTESTS()\nSPLIT_FACTOR(2)\nEND()\n")
        self.write("AnswerTest.java", "package e2e; public class AnswerTest { @org.junit.jupiter.api.Test public void answer() {} }\n")
        graph = self.graph("-t", "--cache-tests")
        self.node(graph, "answer.jar")

    def test_external_jar(self):
        self.write("ya.make", "JAVA_CONTRIB(answer)\nJAR_RESOURCE(123456)\nEND()\n")
        self.node(self.graph(), "answer.jar")
