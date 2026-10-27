from tst.make.lib import MakeTest, PROJECT


class JavaTestWorkersTest(MakeTest):
    def test_parallel_junit_chunks(self):
        self.write("ya.make", "RECURSE(tests)\n")
        self.write("tests/AnswerTest.java", "public class AnswerTest {}\n")
        for module, fork in (("JUNIT5", "FORK_SUBTESTS"), ("JTEST", "FORK_TESTS")):
            with self.subTest(module=module):
                self.write("tests/ya.make", f"{module}(answer)\nJDK_VERSION(17)\nJAVA_SRCS(AnswerTest.java)\n{fork}()\nSPLIT_FACTOR(2)\nPARALLEL_TESTS_WITHIN_NODE(2)\nEND()\n")
                graph = self.graph("-t")
                for index in range(2):
                    work = f"$(B)/{PROJECT}/tests/test-results/{PROJECT}-tests/chunk{index}"
                    node = self.node(graph, f"tests/test-results/{PROJECT}-tests/chunk{index}/meta.json")
                    args = [arg.replace("$(BUILD_ROOT)", "$(B)").replace("$(SOURCE_ROOT)", "$(S)") for cmd in node["cmds"] for arg in cmd["cmd_args"]]
                    start = args.index("run_jtest")
                    self.assertEqual(args[start + 1:start + 5], ["--worker-count", "2", "--temp-tracefile-dir", work + "/temporary_traces"])
                    self.assertTrue(args[start + 5].endswith("/bin/python3"))
                    self.assertEqual(args[start + 6], "$(S)/build/scripts/run_junit.py")
                    self.assertEqual(args[args.index("--modulo") + 1], "2")
                    self.assertEqual(args[args.index("--modulo-index") + 1], str(index))

    def test_parallel_junit_auto_and_single_worker(self):
        self.write("ya.make", "RECURSE(tests)\n")
        self.write("tests/AnswerTest.java", "public class AnswerTest {}\n")
        for workers, requirements, expected in (("auto", "cpu:3", "3"), ("ALL", "cpu:2", "2"), ("auto", "ram:16", None), ("1", "cpu:2", None)):
            with self.subTest(workers=workers, requirements=requirements):
                self.write("tests/ya.make", f"JUNIT5(answer)\nJDK_VERSION(17)\nJAVA_SRCS(AnswerTest.java)\nREQUIREMENTS({requirements})\nPARALLEL_TESTS_WITHIN_NODE({workers})\nEND()\n")
                node = self.node(self.graph("-t"), f"tests/test-results/{PROJECT}-tests/meta.json")
                args = [arg for cmd in node["cmds"] for arg in cmd["cmd_args"]]
                self.assertEqual("run_jtest" in args, expected is not None)
                if expected is not None:
                    self.assertEqual(args[args.index("--worker-count") + 1], expected)
