from tst.make.lib import MakeTest, PROJECT


COMMONS = "contrib/java/org/apache/commons/commons-lang3"


class JavaDependenciesTest(MakeTest):
    def test_uberjar_library_classpath_keeps_its_source_closure(self):
        self.write("ya.make", f"JAVA_PROGRAM(answer)\nJDK_VERSION(17)\nJAVA_SRCS(Main.java)\nPEERDIR({PROJECT}/library)\nEND()\n")
        self.write("Main.java", "public class Main {}\n")
        self.write("library/ya.make", f"JAVA_LIBRARY(library)\nJDK_VERSION(17)\nJAVA_SRCS(Library.java)\nUBERJAR()\nPEERDIR({PROJECT}/leaf)\nEND()\n")
        self.write("library/Library.java", "public class Library {}\n")
        self.write("leaf/ya.make", "JAVA_LIBRARY(leaf)\nJDK_VERSION(17)\nJAVA_SRCS(Leaf.java)\nEND()\n")
        self.write("leaf/Leaf.java", "public class Leaf {}\n")
        node = self.node(self.graph(), "answer.run.cp.jar")
        classpath = next(arg for command in node["cmds"] for arg in command["cmd_args"] if arg.startswith("CLASSPATH="))
        self.assertIn("library.jar", classpath.split("::"))
        self.assertIn("leaf.jar", classpath.split("::"))

    def test_implementation_dependencies_are_hidden_from_transitive_compilation(self):
        peers = {"api": "JAVA_API_PEERDIR", "impl": "JAVA_IMPL_PEERDIR"}
        self.write("ya.make", f"JAVA_PROGRAM(answer)\nJDK_VERSION(17)\nJAVA_SRCS(Main.java)\nPEERDIR({PROJECT}/library)\nEND()\n")
        self.write("Main.java", "public class Main {}\n")
        self.write("library/ya.make", "JAVA_LIBRARY(library)\nJDK_VERSION(17)\nJAVA_SRCS(Library.java)\n" +
                   "".join(f"{macro}({PROJECT}/{peer})\n" for peer, macro in peers.items()) + "END()\n")
        self.write("library/Library.java", "public class Library {}\n")
        for name in peers:
            self.write(f"{name}/ya.make", f"JAVA_LIBRARY({name})\nJDK_VERSION(17)\nJAVA_SRCS(Value.java)\nEND()\n")
            self.write(f"{name}/Value.java", f"package {name}; public class Value {{}}\n")
        graph = self.graph()
        compile_inputs = self.node(graph, "answer.jar")["inputs"]
        self.assertIn(f"$(B)/{PROJECT}/api/api-interface.jar", compile_inputs)
        self.assertNotIn(f"$(B)/{PROJECT}/impl/impl-interface.jar", compile_inputs)
        runtime_inputs = self.node(graph, "answer.run.cp.jar")["inputs"]
        for name in peers:
            self.assertIn(f"$(B)/{PROJECT}/{name}/{name}.jar", runtime_inputs)

    def test_exclusion_aliases_require_matching_java_module(self):
        self.write("Main.java", "public class Main {}\n")
        self.write("lib/ya.make", f"JAVA_LIBRARY(lib)\nJDK_VERSION(17)\nPEERDIR({COMMONS}/3.10)\nEND()\n")
        for declaration, macro in (("JAVA_LIBRARY", "JAVA_RUNTIME_EXCLUDE"),
                                   ("JAVA_LIBRARY", "JAVA_TEST_EXCLUDE"),
                                   ("JAVA_PROGRAM", "JAVA_TEST_EXCLUDE"),
                                   ("JUNIT5", "JAVA_RUNTIME_EXCLUDE")):
            with self.subTest(declaration=declaration, macro=macro):
                self.write("ya.make", f"{declaration}(answer)\nJDK_VERSION(17)\nJAVA_SRCS(Main.java)\nPEERDIR({PROJECT}/lib)\n{macro}({COMMONS})\nEND()\n")
                self.assertNotEqual(self.make("-t").returncode, 0)

    def test_runtime_and_test_exclusions_filter_matching_classpaths(self):
        self.write("Main.java", "public class Main {}\n")
        self.write("lib/ya.make", f"JAVA_LIBRARY(lib)\nJDK_VERSION(17)\nPEERDIR({COMMONS}/3.10)\nEND()\n")
        for declaration, macro, output in (("JAVA_PROGRAM", "JAVA_RUNTIME_EXCLUDE", "answer.run.cp.jar"),
                                           ("JUNIT5", "JAVA_TEST_EXCLUDE", "answer.test.cp.jar")):
            with self.subTest(declaration=declaration):
                self.write("ya.make", f"{declaration}(answer)\nJDK_VERSION(17)\nJAVA_SRCS(Main.java)\nPEERDIR({PROJECT}/lib)\n{macro}({COMMONS})\nEND()\n")
                graph = self.graph("-t") if declaration == "JUNIT5" else self.graph()
                inputs = self.node(graph, output)["inputs"]
                self.assertNotIn(f"$(B)/{COMMONS}/3.10/commons-lang3-3.10.jar", inputs)

    def test_double_trailing_slash_in_exclusion_does_not_match_a_dependency(self):
        self.write("Main.java", "public class Main {}\n")
        self.write("lib/ya.make", f"JAVA_LIBRARY(lib)\nJDK_VERSION(17)\nPEERDIR({COMMONS}/3.10)\nEND()\n")
        self.write("ya.make", f"JAVA_PROGRAM(answer)\nJDK_VERSION(17)\nJAVA_SRCS(Main.java)\nPEERDIR({PROJECT}/lib)\nEXCLUDE({COMMONS}//)\nEND()\n")
        self.assertIn(f"$(B)/{COMMONS}/3.10/commons-lang3-3.10.jar",
                      self.node(self.graph(), "answer.run.cp.jar")["inputs"])

    def test_overlapping_exclusions_preserve_shared_dependencies(self):
        peers = {
            "parent": ("group/upper", "group/lower", "kept"),
            "group/upper": ("group/lower", "leaf"),
            "group/lower": ("leaf", "shared"),
            "kept": ("shared",), "leaf": (), "shared": (),
        }
        for directory, dependencies in peers.items():
            name = directory.rsplit("/", 1)[-1]
            self.write(f"{directory}/ya.make", f"JAVA_LIBRARY({name})\nJDK_VERSION(17)\nJAVA_SRCS(Value.java)\n" +
                       ("PEERDIR(" + " ".join(f"{PROJECT}/{peer}" for peer in dependencies) + ")\n" if dependencies else "") + "END()\n")
            self.write(f"{directory}/Value.java", "public class Value {}\n")
        self.write("Main.java", "public class Main {}\n")
        for suffix in ("", "/"):
            with self.subTest(exclude_suffix=suffix):
                self.write("ya.make", f"JAVA_PROGRAM(answer)\nJDK_VERSION(17)\nJAVA_SRCS(Main.java)\nPEERDIR({PROJECT}/parent)\nEXCLUDE({PROJECT}/group{suffix})\nEND()\n")
                inputs = self.node(self.graph(), "answer.run.cp.jar")["inputs"]
                for directory in peers:
                    name = directory.rsplit("/", 1)[-1]
                    self.assertEqual(f"$(B)/{PROJECT}/{directory}/{name}.jar" in inputs,
                                     directory in ("parent", "kept", "shared"), directory)

    def test_excluded_managed_version_preserves_original_version(self):
        self.write("Main.java", "public class Main {}\n")
        self.write("lib/ya.make", f"JAVA_LIBRARY(lib)\nJDK_VERSION(17)\nJAVA_SRCS(Lib.java)\nPEERDIR({COMMONS}/3.10)\nEND()\n")
        self.write("lib/Lib.java", "public class Lib {}\n")
        self.write("ya.make", f"JAVA_PROGRAM(answer)\nJDK_VERSION(17)\nJAVA_SRCS(Main.java)\nPEERDIR({PROJECT}/lib)\nDEPENDENCY_MANAGEMENT({COMMONS}/3.12.0)\nEXCLUDE({COMMONS}/3.12.0)\nEND()\n")
        inputs = self.node(self.graph(), "answer.run.cp.jar")["inputs"]
        self.assertIn(f"$(B)/{COMMONS}/3.10/commons-lang3-3.10.jar", inputs)
        self.assertNotIn(f"$(B)/{COMMONS}/3.12.0/commons-lang3-3.12.0.jar", inputs)

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
