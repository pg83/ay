import json

from tst.make.lib import MakeTest, PROJECT


class JavaExtendedTest(MakeTest):
    def test_jdk_versions(self):
        self.write("Main.java", "public class Main { public static void main(String[] args) {} }\n")
        for version in (11, 24, 25, 26):
            with self.subTest(version=version):
                self.write("ya.make", f"JAVA_PROGRAM(answer)\nJDK_VERSION({version})\nJAVA_SRCS(Main.java)\nWITH_JDK()\nUSE_ERROR_PRONE()\nEND()\n")
                self.node(self.graph(), "answer.jar")

    def test_kotlin_lint_directories(self):
        self.write("ya.make", "JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nWITH_KOTLIN()\nJAVA_SRCS(src/Answer.kt)\nCOLLECT_KOTLIN_LINT_SRCS(DIRS src DIRS_RECURSE extra)\nSTYLE_DETEKT()\nEND()\n")
        self.write("src/Answer.kt", "class Answer(val value: Int = 42)\n")
        self.write("extra/nested/Other.kt", "class Other\n")
        graph = self.graph("-t")
        self.assertTrue(any("run_custom_lint" in json.dumps(node.get("cmds", [])) and "detekt" in json.dumps(node.get("cmds", [])) for node in graph["graph"]))

    def test_junit_layout_and_runtime_dependencies(self):
        self.write("ya.make", f"JUNIT5(answer)\nJDK_VERSION(17)\nDEFAULT_JUNIT_JAVA_SRCS_LAYOUT()\nJAVA_TEST_PEERDIR({PROJECT}/api)\nJAVA_TEST_PEERDIR({PROJECT}/runtime)\nDATA_FILES({PROJECT}/value.txt)\nTAG(e2e)\nREQUIREMENTS(cpu:1 ram:2)\nTEST_JAVA_CLASSPATH_CMD_TYPE(COMMAND_FILE)\nEND()\n")
        self.write("java/AnswerTest.java", "public class AnswerTest { @org.junit.jupiter.api.Test void answer() {} }\n")
        self.write("resources/answer.txt", "42\n")
        self.write("value.txt", "42\n")
        for name in ("api", "runtime"):
            self.write(name + "/ya.make", f"JAVA_LIBRARY({name})\nJDK_VERSION(17)\nJAVA_SRCS(Value.java)\nEND()\n")
            self.write(name + "/Value.java", f"package {name}; public class Value {{}}\n")
        graph = self.graph("-t")
        self.node(graph, "runtime/runtime.jar")

    def test_java_source_roots(self):
        self.write("shared/Answer.java", "package e2e; public class Answer {}\n")
        self.write("shared/value.txt", "42\n")
        variants = (
            f"SRCDIR({PROJECT}/shared)\nJAVA_SRCS(Answer.java)",
            "SET(SOURCE_DIR shared)\nJAVA_SRCS(SRCDIR ${SOURCE_DIR} *.java)",
            "JAVA_SRCS(SRCDIR ./shared/ *.java)",
            "JAVA_SRCS(SRCDIR shared PACKAGE_PREFIX e2e *.java)",
            "JAVA_SRCS(SRCDIR absent SKIP_CHECK_SRCDIR *.java)\nJAVA_SRCS(shared/*.java)",
        )
        for macros in variants:
            with self.subTest(macros=macros):
                self.write("ya.make", f"JAVA_LIBRARY(answer)\nJDK_VERSION(17)\n{macros}\nEND()\n")
                self.node(self.graph("-t"), "answer.jar")

    def test_jtest(self):
        self.write("ya.make", "JTEST(answer)\nJDK_VERSION(17)\nJAVA_SRCS(AnswerTest.java)\nFORK_TESTS()\nEXPERIMENTAL_FORK()\nEND()\n")
        self.write("AnswerTest.java", "public class AnswerTest { @org.junit.Test public void answer() {} }\n")
        self.node(self.graph("-t"), "answer.jar")

    def test_disable_optional_features(self):
        self.write("ya.make", "JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nWITH_KOTLIN()\nJAVA_SRCS(Answer.kt)\nNO_LINT(ktlint)\nUSE_KTLINT_OLD()\nNO_YMAKE_PYTHON3()\nDISABLE_DATA_VALIDATION()\nENABLE(ENABLE_PREVIEW)\nDISABLE(ENABLE_PREVIEW)\nEND()\n")
        self.write("Answer.kt", "class Answer\n")
        self.node(self.graph("-t"), "answer.jar")

    def test_annotation_processor(self):
        self.write("ya.make", f"JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nJAVA_SRCS(Answer.java)\nANNOTATION_PROCESSOR({PROJECT}/processor)\nEND()\n")
        self.write("Answer.java", "public class Answer {}\n")
        self.write("processor/ya.make", "JAVA_LIBRARY(processor)\nJDK_VERSION(17)\nJAVA_SRCS(Processor.java)\nEND()\n")
        self.write("processor/Processor.java", "public class Processor {}\n")
        self.node(self.graph(), "answer.jar")
