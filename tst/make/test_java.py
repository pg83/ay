from tst.make.lib import MakeTest, PROJECT


class JavaTest(MakeTest):
    def test_recursive_java_sources_and_resources(self):
        self.write("ya.make", "JAVA_LIBRARY(e2e)\nJDK_VERSION(17)\nJAVA_SRCS(SRCDIR src **/*.java)\nJAVA_SRCS(RESOURCES SRCDIR data **/*)\nEND()\n")
        self.write("src/e2e/Hello.java", "package e2e; public class Hello { public static int answer() { return 42; } }\n")
        self.write("data/config/value.txt", "42\n")
        graph = self.graph()
        jar = self.node(graph, "e2e.jar")
        self.assertIn(jar["uid"], graph["result"])
        inputs = {path for node in graph["graph"] for path in node["inputs"]}
        self.assertIn(f"$(S)/{PROJECT}/src/e2e/Hello.java", inputs)
        self.assertIn(f"$(S)/{PROJECT}/data/config/value.txt", inputs)
