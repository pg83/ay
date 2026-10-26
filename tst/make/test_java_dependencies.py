from tst.make.lib import MakeTest, PROJECT


COMMONS = "contrib/java/org/apache/commons/commons-lang3"


class JavaDependenciesTest(MakeTest):
    def test_explicit_java_target_does_not_publish_excluded_test_dependencies(self):
        makefile = f"PY3TEST()\nTEST_SRCS(test_answer.py)\nDEPENDS({PROJECT}/program)\nEND()\n"
        self.write("test_answer.py", "def test_answer():\n    assert 6 * 7 == 42\n")
        self.write("program/ya.make", f"JAVA_PROGRAM(answer)\nJDK_VERSION(17)\nJAVA_SRCS(Main.java)\nPEERDIR({PROJECT}/lib)\nEXCLUDE({COMMONS})\nEND()\n")
        self.write("program/Main.java", "public class Main {}\n")
        self.write("lib/ya.make", f"JAVA_CONTRIB(lib)\nLOCAL_JAR(lib.jar)\nPEERDIR({COMMONS}/3.10)\nEND()\n")
        (self.project / "lib/lib.jar").write_bytes(b"PK\x05\x06" + b"\0" * 18)
        for mode in ("depends", "recurse", "explicit-first", "explicit-last"):
            with self.subTest(mode=mode):
                self.write("ya.make", makefile + ("RECURSE(program)\n" if mode == "recurse" else ""))
                explicit = mode.startswith("explicit")
                targets = ([PROJECT] if mode == "explicit-last" else []) + ([f"{PROJECT}/program"] if explicit else [])
                graph = self.graph("-t", "--add-modules-to-results", *targets)
                self.node(graph, "program/answer.jar")
                outputs = {path for node in graph["graph"] for path in node["outputs"]}
                self.assertEqual(f"$(B)/{COMMONS}/3.10/commons-lang3-3.10.jar" in outputs, not explicit)
                self.assertEqual(f"$(B)/{PROJECT}/test-results/py3test/meta.json" in outputs, not explicit)

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
