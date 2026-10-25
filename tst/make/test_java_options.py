import json

from tst.make.lib import MakeTest, PROJECT


class JavaOptionsTest(MakeTest):
    def test_compiler_options(self):
        for macros in ("USE_ERROR_PRONE()\nJAVAC_FLAGS(-Xlint:deprecation)", "ENABLE_PREVIEW()\nNO_LINT()", "WITH_KOTLIN()\nWITH_KOTLINC_DETEKT()\nKOTLINC_FLAGS(-nowarn)", "WITH_KOTLIN()\nWITH_KOTLINC_LOMBOK()\nENABLE_KOTLIN_ABI_JAR()"):
            with self.subTest(macros=macros):
                self.write("ya.make", f"JAVA_LIBRARY(answer)\nJDK_VERSION(21)\n{macros}\nJAVA_SRCS(Answer.java)\nEND()\n")
                self.write("Answer.java", "public class Answer {}\n")
                self.node(self.graph(), "answer.jar")

    def test_uberjar_transformers(self):
        self.write("ya.make", "JAVA_PROGRAM(answer)\nJDK_VERSION(17)\nJAVA_SRCS(Main.java)\nUBERJAR()\nUBERJAR_HIDING_PREFIX(hidden)\nUBERJAR_HIDE_EXCLUDE_PATTERN(e2e.*)\nUBERJAR_HIDE_INCLUDE_PATTERN(other.*)\nUBERJAR_PATH_EXCLUDE_PREFIX(META-INF/unused)\nUBERJAR_MANIFEST_TRANSFORMER_MAIN(Main)\nUBERJAR_MANIFEST_TRANSFORMER_ATTRIBUTE(Answer 42)\nUBERJAR_APPENDING_TRANSFORMER(META-INF/answer)\nUBERJAR_SERVICES_RESOURCE_TRANSFORMER()\nEND()\n")
        self.write("Main.java", "public class Main { public static void main(String[] args) {} }\n")
        self.node(self.graph(), "answer.jar")

    def test_default_source_layout(self):
        self.write("ya.make", "JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nDEFAULT_JAVA_SRCS_LAYOUT()\nEND()\n")
        self.write("src/main/java/e2e/Answer.java", "package e2e; public class Answer {}\n")
        self.write("src/main/resources/value.txt", "42\n")
        graph = self.graph()
        self.assertTrue(any(f"$(S)/{PROJECT}/src/main/java/e2e/Answer.java" in node["inputs"] for node in graph["graph"]))

    def test_junit6(self):
        self.write("ya.make", f"JUNIT6()\nJDK_VERSION(21)\nJAVA_SRCS(AnswerTest.java)\nFORK_SUBTESTS()\nSPLIT_FACTOR(2)\nENV(E2E=42)\nJVM_ARGS(-Xmx256m)\nJAVA_IGNORE_CLASSPATH_CLASH_FOR(e2e.Answer)\nDATA(arcadia/{PROJECT}/value.txt)\nEND()\n")
        self.write("AnswerTest.java", "public class AnswerTest { @org.junit.jupiter.api.Test void answer() {} }\n")
        self.write("value.txt", "42\n")
        graph = self.graph("-t")
        self.assertTrue(any("junit" in json.dumps(node.get("cmds", [])) for node in graph["graph"]))

    def test_jtest_for(self):
        self.write("ya.make", f"JTEST_FOR({PROJECT}/lib)\nJDK_VERSION(17)\nJAVA_SRCS(AnswerTest.java)\nEND()\n")
        self.write("AnswerTest.java", "public class AnswerTest { @org.junit.Test public void answer() {} }\n")
        self.write("lib/ya.make", "JAVA_LIBRARY(lib)\nJDK_VERSION(17)\nJAVA_SRCS(Answer.java)\nEND()\n")
        self.write("lib/Answer.java", "public class Answer {}\n")
        self.graph("-t")

    def test_java_grpc(self):
        self.write("ya.make", f"JAVA_PROGRAM(answer)\nJDK_VERSION(17)\nJAVA_SRCS(Main.java)\nPEERDIR({PROJECT}/proto)\nEND()\n")
        self.write("Main.java", "public class Main { public static void main(String[] args) {} }\n")
        self.write("proto/ya.make", "PROTO_LIBRARY()\nGRPC()\nWITH_KOTLIN_GRPC()\nPROTOC_FATAL_WARNINGS()\nSRCS(answer.proto)\nEND()\n")
        self.write("proto/answer.proto", 'syntax = "proto3"; package e2e; message Answer { int32 value = 1; } service Answers { rpc Get(Answer) returns (Answer); }\n')
        graph = self.graph()
        self.assertTrue(any(f"$(S)/{PROJECT}/proto/answer.proto" in node["inputs"] and any(path.endswith(".jsrc") for path in node["outputs"]) for node in graph["graph"]))
