import unittest

from tst import lib
from tst.make.test_java_tests import head_args, junit_tree, option_values, run_args, test_nodes


class JavaJunitMetadataTest(unittest.TestCase):
    def tree(self, body, **extra):
        return junit_tree(body, **{
            "devtools/junit-runner/ya.make": "JAVA_LIBRARY_NON_SPLIT()\nEND()\n",
            "devtools/junit6-runner/ya.make": "JAVA_LIBRARY_NON_SPLIT()\nEND()\n",
            "devtools/jtest-annotations/junit4/ya.make": "JAVA_LIBRARY_NON_SPLIT()\nEND()\n",
            "devtools/jtest-annotations/junit6/ya.make": "JAVA_LIBRARY_NON_SPLIT()\nEND()\n",
            **extra,
        })

    def assert_selected_jar(self, graph, jar):
        suite = test_nodes(graph)["proj/sub/ut/sub-ut"]
        args = run_args(suite)
        cpf = jar.removesuffix(".jar") + ".test.cpf"
        self.assertEqual(option_values(args, "--tests-jar-path"), [jar])
        self.assertEqual(option_values(args, "--tests-jar"), [jar])
        self.assertEqual(option_values(args, "-classpath"), ["@" + cpf])
        writer = suite["cmds"][1]["cmd_args"]
        self.assertEqual(option_values(writer, "--file"), [cpf])
        self.assertEqual(suite["cmds"][0]["cmd_args"][-1], cpf.rsplit("/", 1)[0])
        self.assertIn("-DBINDIR=$(B)/proj/sub/ut", args)
        self.assertIn("-DCURDIR=$(S)/proj/sub/ut", args)
        self.assertEqual(option_values(head_args(suite), "--project-path"), ["proj/sub/ut"])
        self.assertIn("$(B)/proj/sub/ut/test-results/sub-ut/meta.json", suite["outputs"])
        producer = lib.node_by_output(graph, jar)
        self.assertIn(producer["uid"], suite["deps"])
        return suite

    def test_unselected_junit_uses_its_own_jar(self):
        for module in ("JTEST", "JUNIT5", "JUNIT6"):
            with self.subTest(module=module):
                files = self.tree(module + "()\nEND()\n")
                graph = lib.make(files, "proj/sub/ut", "--target-platform-tests")
                suite = self.assert_selected_jar(graph, "$(B)/proj/sub/ut/proj-sub-ut.jar")
                self.assertEqual(option_values(head_args(suite), "--test-run-cwd"), [])

    def test_jtest_for_uses_the_selected_peer_target_without_default_cwd(self):
        files = self.tree("JTEST_FOR(subject)\nEND()\n", **{
            "subject/ya.make": "JAVA_LIBRARY(subject_tests PREFIX selected_)\nEND()\n",
        })
        lib.tool_program(files, "contrib/tools/ijar", "ijar")
        graph = lib.make(files, "proj/sub/ut", "--target-platform-tests")
        suite = self.assert_selected_jar(graph, "$(B)/subject/selected_subject_tests.jar")
        self.assertEqual(option_values(head_args(suite), "--test-run-cwd"), [])
        classpath = suite["cmds"][1]["cmd_args"]
        self.assertIn("proj/sub/ut/proj-sub-ut.jar", classpath)
        self.assertIn("subject/selected_subject_tests.jar", classpath)

    def test_explicit_test_jar_uses_last_selected_peer_real_target(self):
        for module in ("JTEST", "JUNIT5", "JUNIT6"):
            with self.subTest(module=module):
                files = self.tree(module + "()\nJUNIT_TESTS_JAR(first ignored.jar)\n"
                                  "JUNIT_TESTS_JAR(second also-ignored.jar)\nEND()\n", **{
                    "first/ya.make": "JAVA_LIBRARY_NON_SPLIT(first_tests)\nEND()\n",
                    "second/ya.make": "JAVA_LIBRARY_NON_SPLIT(real_tests PREFIX chosen_)\nEND()\n",
                })
                graph = lib.make(files, "proj/sub/ut", "--target-platform-tests")
                suite = self.assert_selected_jar(graph, "$(B)/second/chosen_real_tests.jar")
                classpath = suite["cmds"][1]["cmd_args"]
                self.assertIn("first/first_tests.jar", classpath)
                self.assertIn("proj/sub/ut/proj-sub-ut.jar", classpath)

    def test_test_cwd_is_explicit_expanded_and_last_value_wins(self):
        for header, jar in (("JTEST_FOR(lib)", "$(B)/lib/lib.jar"),
                            ("JUNIT5()", "$(B)/proj/sub/ut/proj-sub-ut.jar")):
            with self.subTest(header=header):
                files = self.tree(header + "\nSET(TEST_DIRECTORY fixtures)\n"
                                  "TEST_CWD(unused)\nTEST_CWD($TEST_DIRECTORY)\nEND()\n", **{
                    "fixtures/input.txt": "test data\n",
                })
                graph = lib.make(files, "proj/sub/ut", "--target-platform-tests")
                suite = self.assert_selected_jar(graph, jar)
                self.assertEqual(option_values(head_args(suite), "--test-run-cwd"), ["$(S)/fixtures"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
