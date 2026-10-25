from tst.make.lib import MakeTest, PROJECT


COMMONS = "contrib/java/org/apache/commons/commons-lang3"


class JavaDependenciesTest(MakeTest):
    def test_managed_version_replaces_transitive_version(self):
        self.write("ya.make", f"JAVA_PROGRAM(answer)\nJDK_VERSION(17)\nJAVA_SRCS(Main.java)\nPEERDIR({PROJECT}/lib)\nDEPENDENCY_MANAGEMENT({COMMONS}/3.12.0)\nEND()\n")
        self.write("Main.java", "public class Main { public static void main(String[] args) {} }\n")
        self.write("lib/ya.make", f"JAVA_LIBRARY(lib)\nJDK_VERSION(17)\nJAVA_SRCS(Lib.java)\nPEERDIR({COMMONS}/3.10)\nEND()\n")
        self.write("lib/Lib.java", "public class Lib {}\n")
        graph = self.graph()
        outputs = {path for node in graph["graph"] for path in node["outputs"]}
        self.assertTrue(any(f"/{COMMONS}/3.12.0/" in path and path.endswith(".jar") for path in outputs))

    def test_excluded_transitive_dependency(self):
        self.write("ya.make", f"JAVA_PROGRAM(answer)\nJDK_VERSION(17)\nJAVA_SRCS(Main.java)\nPEERDIR({PROJECT}/lib)\nEXCLUDE({COMMONS})\nEND()\n")
        self.write("Main.java", "public class Main { public static void main(String[] args) {} }\n")
        self.write("lib/ya.make", f"JAVA_LIBRARY(lib)\nJDK_VERSION(17)\nJAVA_SRCS(Lib.java)\nPEERDIR({COMMONS}/3.10)\nEND()\n")
        self.write("lib/Lib.java", "public class Lib {}\n")
        self.node(self.graph(), "answer.jar")

    def test_kotlin_compiler_plugins(self):
        self.write("ya.make", "JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nWITH_KOTLIN()\nWITH_KOTLINC_ALLOPEN(annotation=e2e.Open)\nWITH_KOTLINC_NOARG(annotation=e2e.Open)\nWITH_KOTLINC_SERIALIZATION()\nJAVA_SRCS(Answer.kt)\nEND()\n")
        self.write("Answer.kt", "package e2e\nannotation class Open\n@Open class Answer(val value: Int = 42)\n")
        self.node(self.graph(), "answer.jar")

    def test_kapt(self):
        self.write("ya.make", "JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nWITH_KOTLIN()\nWITH_KAPT(GENERATE_KOTLIN)\nJAVA_SRCS(Answer.kt)\nEND()\n")
        self.write("Answer.kt", "package e2e\nclass Answer(val value: Int = 42)\n")
        self.node(self.graph(), "answer.jar")
