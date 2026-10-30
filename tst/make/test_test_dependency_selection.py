from tst.make.lib import MakeTest, PROJECT


class TestDependencySelectionTest(MakeTest):
    def consumer(self, directory):
        self.write("ya.make", f"EXECTEST()\nDEPENDS({PROJECT}/{directory})\nRUN(echo answer)\nEND()\n")

    def test_dependencies_require_a_module_declaration(self):
        self.consumer("empty")
        self.write("empty/ya.make", "SET(E2E_VALUE 42)\n")
        self.assertNotEqual(self.make("-t").returncode, 0)

    def test_dependencies_can_select_java_proto_schema_variants(self):
        self.consumer("schema")
        self.write("schema/ya.make", "PROTO_SCHEMA()\nONLY_TAGS(JAVA_PROTO_FROM_SCHEMA)\nSRCS(answer.proto)\nEND()\n")
        self.write("schema/answer.proto", 'syntax = "proto3"; package e2e; message Answer { int32 value = 1; }\n')
        graph = self.graph("-t", "--add-modules-to-results")
        self.node(graph, "test-results/exectest/meta.json")
        outputs = [path for node in graph["graph"] for path in node["outputs"]]
        self.assertNotIn(f"$(B)/{PROJECT}/schema/answer.pb.cc", outputs)

    def test_java_dependencies_can_select_only_the_compilation_variant(self):
        self.consumer("java")
        self.write("java/ya.make", "JAVA_PROGRAM(answer)\nJDK_VERSION(17)\nONLY_TAGS(JAR_COMPILATION)\nJAVA_SRCS(Main.java)\nEND()\n")
        self.write("java/Main.java", "public class Main { public static void main(String[] args) {} }\n")
        graph = self.graph("-t", "--add-modules-to-results")
        self.node(graph, "test-results/exectest/meta.json")
        self.assertFalse(any(path.endswith("/answer.run.cp.jar")
                             for node in graph["graph"] for path in node["outputs"]))
