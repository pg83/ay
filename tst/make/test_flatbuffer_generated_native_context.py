from tst.make.lib import MakeTest, PROJECT


class FlatbufferGeneratedNativeContextTest(MakeTest):
    def test_native_and_java_views_keep_a_generated_native_source_producer(self):
        self.write("ya.make", "RECURSE(native java)\n")
        self.write("native/ya.make", f"PROGRAM(answer)\nSRCS(main.cpp)\nPEERDIR({PROJECT}/schema)\nEND()\n")
        self.write("native/main.cpp", "int main() { return 0; }\n")
        self.write("java/ya.make", f"JAVA_PROGRAM(answer)\nJDK_VERSION(17)\nJAVA_SRCS(Answer.java)\nPEERDIR({PROJECT}/schema)\nEND()\n")
        self.write("java/Answer.java", "public class Answer { public static void main(String[] args) {} }\n")
        self.write("schema/ya.make", "FBS_LIBRARY(shared)\nSRCS(answer.fbs)\nRUN_PYTHON3(generate.py ${BINDIR}/generated.cpp OUT generated.cpp)\nEND()\n")
        self.write("schema/answer.fbs", "namespace e2e; table Answer { value:int; } root_type Answer;\n")
        self.write("schema/generate.py", "import pathlib, sys\npath = pathlib.Path(sys.argv[1])\npath.parent.mkdir(parents=True, exist_ok=True)\npath.write_text('int answer() { return 42; }\\n')\n")
        graph = self.graph()
        generated = self.node(graph, "schema/generated.cpp")
        compiled = self.node(graph, "schema/generated.cpp.o")
        self.assertIn(f"$(B)/{PROJECT}/schema/generated.cpp", compiled["inputs"])
        self.assertIn(generated["uid"], compiled["deps"])
        self.node(graph, "native/answer")
        self.node(graph, "java/answer.jar")
