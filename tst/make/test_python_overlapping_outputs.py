from tst.make.lib import MakeTest


class PythonOverlappingOutputsTest(MakeTest):
    def test_overlapping_module_outputs_do_not_drop_python_import_checks(self):
        self.write("ya.make", "RECURSE(first first/sub)\n")
        self.write("first/ya.make", "PY2_PROGRAM(sub/answer)\nPY_SRCS(MAIN main.py)\nEND()\n")
        self.write("first/main.py", "print(42)\n")
        self.write("first/sub/ya.make", "PROGRAM(answer)\nSRCS(main.cpp)\nEND()\n")
        self.write("first/sub/main.cpp", "int main() { return 0; }\n")
        graph = self.graph("-t", "-k")
        self.node(graph, "first/test-results/import_test/meta.json")
