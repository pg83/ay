from tst.make.lib import MakeTest, PROJECT


class JavaTestSuiteEdgesTest(MakeTest):
    def test_library_variants_check_classpath_clashes(self):
        self.write("Answer.java", "public class Answer {}\n")
        for module in ("JAVA_LIBRARY", "JAVA_LIBRARY_NON_SPLIT"):
            with self.subTest(module=module):
                self.write("ya.make", f"{module}(answer)\nJDK_VERSION(17)\nJAVA_SRCS(Answer.java)\nCHECK_JAVA_DEPS(strict)\nEND()\n")
                graph = self.graph("-t")
                checks = [command["cmd_args"] for node in graph["graph"] for command in node["cmds"] if "run_classpath_clash" in command["cmd_args"]]
                self.assertEqual(len(checks), 1)
                self.assertIn("--strict", checks[0])

    def test_junit_native_peers_are_on_the_library_search_path(self):
        self.write("ya.make", f"JUNIT5(answer)\nJDK_VERSION(17)\nJAVA_SRCS(AnswerTest.java)\nPEERDIR({PROJECT}/native)\nEND()\n")
        self.write("AnswerTest.java", "public class AnswerTest {}\n")
        self.write("native/ya.make", "DLL(native)\nSRCS(answer.cpp)\nEND()\n")
        self.write("native/answer.cpp", 'extern "C" int answer() { return 42; }\n')
        graph = self.graph("-t")
        args = [arg.replace("$(BUILD_ROOT)", "$(B)") for node in graph["graph"] for command in node["cmds"] for arg in command["cmd_args"]]
        for property in ("java.library.path", "jna.library.path"):
            self.assertIn(f"-D{property}=$(B)/{PROJECT}/native", args)
        self.node(graph, "native/libnative.so")

    def test_old_ktlint_uses_its_own_editorconfig(self):
        self.write("ya.make", "JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nWITH_KOTLIN()\nUSE_KTLINT_OLD()\nJAVA_SRCS(Answer.kt)\nEND()\n")
        self.write("Answer.kt", "class Answer\n")
        node = self.node(self.graph("-t"), "test-results/ktlint/meta.json")
        args = [arg.replace("$(SOURCE_ROOT)", "$(S)") for command in node["cmds"] for arg in command["cmd_args"]]
        self.assertEqual(args[args.index("--editorconfig") + 1], "$(S)/build/platform/java/ktlint_old/.editorconfig")

    def test_junit_worker_count_must_be_positive(self):
        self.write("AnswerTest.java", "public class AnswerTest {}\n")
        for workers in ("0", "-1"):
            with self.subTest(workers=workers):
                self.write("ya.make", f"JUNIT5(answer)\nJDK_VERSION(17)\nJAVA_SRCS(AnswerTest.java)\nPARALLEL_TESTS_WITHIN_NODE({workers})\nEND()\n")
                self.assertNotEqual(self.make("-t").returncode, 0)
