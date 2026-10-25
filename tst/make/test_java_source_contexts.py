from tst.make.lib import MakeTest, PROJECT


class JavaSourceContextsTest(MakeTest):
    def test_generated_source_directory(self):
        self.write("ya.make", f"JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nRUN_JAVA_PROGRAM(Generator CLASSPATH {PROJECT}/tool OUT_DIR generated IN_DIR data CWD ${{BINDIR}})\nJAVA_SRCS(SRCDIR ${{BINDIR}}/generated **/*.java)\nEND()\n")
        self.write("data/value.txt", "42\n")
        self.write("tool/ya.make", "JAVA_PROGRAM(generator)\nJDK_VERSION(17)\nJAVA_SRCS(Generator.java)\nEND()\n")
        self.write("tool/Generator.java", 'public class Generator { public static void main(String[] args) throws Exception { java.nio.file.Files.createDirectories(java.nio.file.Path.of("generated")); java.nio.file.Files.writeString(java.nio.file.Path.of("generated/Answer.java"), "public class Answer {}\\n"); } }\n')
        graph = self.graph()
        self.node(graph, "answer.jar")
        self.assertTrue(any(f"$(S)/{PROJECT}/data/value.txt" in node["inputs"] for node in graph["graph"]))

    def test_duplicate_source_roots(self):
        self.write("ya.make", f"JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nJAVA_SRCS(SRCDIR ${{ARCADIA_ROOT}}/{PROJECT}/src **/*.java)\nJAVA_SRCS(SRCDIR ${{ARCADIA_ROOT}}/{PROJECT}/src **/*.java)\nJAVA_EXTERNAL_DEPENDENCIES(value.txt)\nNO_COW()\nNO_NEED_CHECK()\nEND()\n")
        self.write("src/Answer.java", "public class Answer {}\n")
        self.write("value.txt", "42\n")
        self.node(self.graph(), "answer.jar")

    def test_kotlin_modern_jdk_and_abi(self):
        self.write("ya.make", "JAVA_LIBRARY(answer)\nJDK_VERSION(25)\nWITH_KOTLIN()\nENABLE_KOTLIN_ABI_JAR()\nWITH_KAPT()\nKAPT_ANNOTATION_PROCESSOR(Example)\nKAPT_OPTS(plugin:org.jetbrains.kotlin.kapt3:correctErrorTypes=true)\nWITH_KOTLINC_DETEKT(debug=false)\nJAVA_SRCS(Answer.kt)\nEND()\n")
        self.write("Answer.kt", "class Answer\n")
        self.node(self.graph(), "answer.jar")

    def test_test_runtime_recipe(self):
        self.write("ya.make", f"JUNIT5(answer)\nJDK_VERSION(17)\nSIZE(MEDIUM)\nJAVA_SRCS(AnswerTest.java)\nDEPENDS({PROJECT}/recipe)\nUSE_RECIPE({PROJECT}/recipe/recipe)\nDOCKER_IMAGE(docker://registry.example.com/test@sha256:{'a'*64}=test)\nEND()\n")
        self.write("AnswerTest.java", "public class AnswerTest {}\n")
        self.write("recipe/ya.make", "PY3_PROGRAM(recipe)\nPY_SRCS(MAIN main.py)\nEND()\n")
        self.write("recipe/main.py", "pass\n")
        self.graph("-t")

    def test_java_generator_missing_outputs(self):
        self.write("ya.make", f"JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nJAVA_SRCS(Answer.java)\nRUN_JAVA_PROGRAM(Generator CLASSPATH {PROJECT}/tool)\nEND()\n")
        self.write("Answer.java", "public class Answer {}\n")
        self.write("tool/ya.make", "JAVA_PROGRAM(tool)\nJDK_VERSION(17)\nJAVA_SRCS(Generator.java)\nEND()\n")
        self.write("tool/Generator.java", "public class Generator {}\n")
        self.assertNotEqual(self.make().returncode, 0)
