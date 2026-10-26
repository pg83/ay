import json

from tst.make.lib import MakeTest, PROJECT


class JavaContextsTest(MakeTest):
    def test_source_exclusions(self):
        self.write("ya.make", "JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nJAVA_SRCS(SRCDIR src **/*.java EXCLUDE **/Ignored.java)\nEND()\n")
        self.write("src/e2e/Answer.java", "package e2e; public class Answer {}\n")
        self.write("src/e2e/Ignored.java", "package e2e; public class Ignored {}\n")
        graph = self.graph()
        inputs = {path for node in graph["graph"] for path in node["inputs"]}
        self.assertIn(f"$(S)/{PROJECT}/src/e2e/Answer.java", inputs)
        self.assertNotIn(f"$(S)/{PROJECT}/src/e2e/Ignored.java", inputs)

    def test_classpath_check(self):
        self.write("ya.make", f"JAVA_PROGRAM(answer)\nJDK_VERSION(17)\nJAVA_SRCS(Main.java)\nPEERDIR({PROJECT}/lib)\nCHECK_JAVA_DEPS(strict)\nJAVA_IGNORE_CLASSPATH_CLASH_FOR(e2e.Duplicate)\nEND()\n")
        self.write("Main.java", "public class Main { public static void main(String[] args) {} }\n")
        self.write("lib/ya.make", "JAVA_LIBRARY(lib)\nJDK_VERSION(17)\nJAVA_SRCS(Lib.java)\nEND()\n")
        self.write("lib/Lib.java", "public class Lib {}\n")
        self.assertTrue(any("run_classpath_clash" in json.dumps(node.get("cmds", [])) for node in self.graph("-t")["graph"]))

    def test_generator_variables(self):
        self.write("ya.make", f"JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nSET(INPUT input.txt)\nDEFAULT(OUTPUT Generated.java)\nSET(ARGUMENTS ${{INPUT}} ${{OUTPUT}})\nRUN_JAVA_PROGRAM(e2e.Generator ${{ARGUMENTS}} CLASSPATH {PROJECT}/tool IN ${{INPUT}} OUT ${{OUTPUT}})\nJAVA_SRCS(${{OUTPUT}})\nEND()\n")
        self.write("input.txt", "42\n")
        self.write("tool/ya.make", "JAVA_PROGRAM(generator)\nJDK_VERSION(17)\nJAVA_SRCS(Generator.java)\nEND()\n")
        self.write("tool/Generator.java", 'package e2e; public class Generator { public static void main(String[] args) throws Exception { java.nio.file.Files.writeString(java.nio.file.Path.of(args[1]), "public class Generated {}\\n"); } }\n')
        self.node(self.graph(), "Generated.java")

    def test_copied_source(self):
        self.write("ya.make", "JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nCOPY_FILE(Original.java Generated.java)\nJAVA_SRCS(Generated.java)\nEND()\n")
        self.write("Original.java", "class Generated {}\n")
        graph = self.graph()
        self.node(graph, "Generated.java")
        self.node(graph, "answer.jar")

    def test_sandbox_source(self):
        self.write("ya.make", "JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nFROM_SANDBOX(FILE 123456789 RENAME Generated.java OUT_NOAUTO Generated.java)\nJAVA_SRCS(Generated.java)\nEND()\n")
        self.node(self.graph(), "Generated.java")

    def test_source_jar_resource(self):
        self.write("ya.make", "JAVA_CONTRIB(answer)\nJAR_RESOURCE(123456789)\nSRC_RESOURCE(123456790)\nEND()\n")
        self.node(self.graph(), "answer.jar")

    def test_external_test_jar(self):
        self.write("ya.make", f"JUNIT5(answer)\nJDK_VERSION(17)\nJUNIT_TESTS_JAR({PROJECT}/tests tests.jar)\nTIMEOUT(30)\nTEST_CWD({PROJECT})\nSYSTEM_PROPERTIES(FILE {PROJECT}/properties.txt)\nEND()\n")
        self.write("tests/ya.make", "JAVA_LIBRARY(tests)\nJDK_VERSION(17)\nJAVA_SRCS(AnswerTest.java)\nEND()\n")
        self.write("tests/AnswerTest.java", "public class AnswerTest {}\n")
        self.write("properties.txt", "e2e.answer=42\n")
        self.assertTrue(any("junit" in json.dumps(node.get("cmds", [])) for node in self.graph("-t")["graph"]))

    def test_api_and_implementation_peers(self):
        self.write("ya.make", f"JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nJAVA_SRCS(Answer.java)\nJAVA_API_PEERDIR({PROJECT}/api)\nJAVA_IMPL_PEERDIR({PROJECT}/impl)\nEND()\n")
        self.write("Answer.java", "public class Answer {}\n")
        for name in ("api", "impl"):
            self.write(f"{name}/ya.make", f"JAVA_LIBRARY({name})\nJDK_VERSION(17)\nJAVA_SRCS(Value.java)\nEND()\n")
            self.write(f"{name}/Value.java", f"package {name}; public class Value {{}}\n")
        self.node(self.graph(), "answer.jar")

    def test_kapt_processor_classpath(self):
        self.write("ya.make", f"JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nWITH_KOTLIN()\nWITH_KAPT()\nKAPT_ANNOTATION_PROCESSOR_CLASSPATH({PROJECT}/processor)\nKAPT_ANNOTATION_PROCESSOR_OPTIONS(e2e=42)\nJAVA_SRCS(Answer.kt)\nEND()\n")
        self.write("Answer.kt", "class Answer\n")
        self.write("processor/ya.make", "JAVA_LIBRARY(processor)\nJDK_VERSION(17)\nJAVA_SRCS(Processor.java)\nEND()\n")
        self.write("processor/Processor.java", "public class Processor {}\n")
        self.node(self.graph(), "answer.jar")

    def test_event_proto_peer(self):
        self.write("ya.make", f"JAVA_PROGRAM(answer)\nJDK_VERSION(17)\nJAVA_SRCS(Answer.java)\nPEERDIR({PROJECT}/proto)\nEND()\n")
        self.write("Answer.java", "public class Answer {}\n")
        self.write("proto/ya.make", "PROTO_LIBRARY()\nSRCS(answer.ev)\nEND()\n")
        self.write("proto/answer.ev", 'syntax = "proto2"; package e2e; message Answer { optional int32 value = 1; }\n')
        graph = self.graph()
        self.assertTrue(any(f"$(S)/{PROJECT}/proto/answer.ev" in node["inputs"] for node in graph["graph"]), [path for node in graph["graph"] for path in node["outputs"] if PROJECT in path])
        proto_jars = [node for node in graph["graph"] if any(path.startswith(f"$(B)/{PROJECT}/proto/") and path.endswith(".jar") for path in node["outputs"])]
        self.assertTrue(any("$(B)/library/cpp/eventlog/proto/cpp-eventlog-proto.jar" in node["inputs"] for node in proto_jars))
