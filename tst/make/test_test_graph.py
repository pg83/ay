import json

from tst.make.lib import MakeTest, PROJECT


class TestGraphTest(MakeTest):
    def assert_test_command(self, graph, kind):
        commands = [json.dumps(node.get("cmds", [])) for node in graph["graph"]]
        self.assertTrue(any("run_test" in command and kind in command for command in commands))

    def test_cpp_test_metadata(self):
        self.write("ya.make", f"""UNITTEST()
SRCS(test.cpp)
SIZE(SMALL)
TIMEOUT(30)
TAG(e2e)
REQUIREMENTS(cpu:1 ram:1)
DATA(arcadia/{PROJECT}/data.txt)
ENV(E2E_VALUE=42)
FORK_SUBTESTS()
SPLIT_FACTOR(2)
END()
""")
        self.write("test.cpp", '#include <library/cpp/testing/unittest/registar.h>\nY_UNIT_TEST_SUITE(Example) { Y_UNIT_TEST(Answer) { UNIT_ASSERT_EQUAL(42, 42); } }\n')
        self.write("data.txt", "42\n")
        graph = self.graph("-t")
        self.node(graph, "test.cpp.o")
        self.assert_test_command(graph, "unittest")

    def test_python_test_graph(self):
        self.write("ya.make", "PY3TEST()\nTEST_SRCS(test_answer.py)\nNO_DOCTESTS()\nFORK_TEST_FILES()\nEND()\n")
        self.write("test_answer.py", "def test_answer():\n    assert 6 * 7 == 42\n")
        graph = self.graph("-t", "--tests-retries=2")
        self.assert_test_command(graph, "py3test")

    def test_go_test_graph(self):
        self.write("ya.make", "RECURSE(lib/gotest)\n")
        self.write("lib/ya.make", "GO_LIBRARY()\nSRCS(answer.go)\nGO_TEST_SRCS(answer_test.go)\nEND()\n")
        self.write("lib/answer.go", "package answer\nfunc Answer() int { return 42 }\n")
        self.write("lib/answer_test.go", 'package answer\nimport "testing"\nfunc TestAnswer(t *testing.T) { if Answer() != 42 { t.Fatal("wrong answer") } }\n')
        self.write("lib/gotest/ya.make", f"GO_TEST_FOR({PROJECT}/lib)\nEND()\n")
        self.assert_test_command(self.graph("-t"), "go_test")

    def test_go_test_direct_target(self):
        self.write("ya.make", f"GO_TEST_FOR({PROJECT}/lib)\nEND()\n")
        self.write("lib/ya.make", "GO_LIBRARY()\nSRCS(answer.go)\nGO_TEST_SRCS(answer_test.go)\nEND()\n")
        self.write("lib/answer.go", "package answer\nfunc Answer() int { return 42 }\n")
        self.write("lib/answer_test.go", 'package answer\nimport "testing"\nfunc TestAnswer(t *testing.T) { if Answer() != 42 { t.Fatal("wrong answer") } }\n')
        self.assert_test_command(self.graph("-t"), "go_test")

    def test_junit_graph(self):
        self.write("ya.make", "JUNIT5(e2e)\nJDK_VERSION(17)\nJAVA_SRCS(SRCDIR src **/*.java)\nEND()\n")
        self.write("src/e2e/AnswerTest.java", "package e2e; import org.junit.jupiter.api.Test; public class AnswerTest { @Test public void answer() { org.junit.jupiter.api.Assertions.assertEquals(42, 6 * 7); } }\n")
        self.assert_test_command(self.graph("-t"), "junit")
