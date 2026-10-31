from tst.make.lib import MakeTest, PROJECT


class JavaFlatbufferNativeInputsTest(MakeTest):
    def test_java_flatbuffer_native_object_can_be_an_explicit_generator_input(self):
        self.write("ya.make", "RECURSE(java consumer)\n")
        self.write("java/ya.make", f"JAVA_LIBRARY(subject)\nPEERDIR({PROJECT}/schema)\nJAVA_SRCS(Subject.java)\nEND()\n")
        self.write("java/Subject.java", "public class Subject {}\n")
        self.write("schema/ya.make", "FBS_LIBRARY(schema)\nONLY_TAGS(JAVA_FBS)\nSRCS(answer.fbs native.cpp)\nEND()\n")
        self.write("schema/answer.fbs", "namespace e2e; table Answer { value:int; } root_type Answer;\n")
        self.write("schema/native.cpp", "int native_answer() { return 42; }\n")
        self.write("consumer/ya.make", f"LIBRARY(consumer)\nRUN_PYTHON3(generate.py IN ${{ARCADIA_BUILD_ROOT}}/{PROJECT}/schema/native.cpp.o STDOUT generated.cpp)\nEND()\n")
        self.write("consumer/generate.py", 'print("int answer() { return 42; }")\n')
        graph = self.graph(env={"AY_DEBUG_PENDING": "1"})
        native = self.node(graph, "schema/native.cpp.o")
        generated = self.node(graph, "consumer/generated.cpp")
        self.assertIn(f"$(S)/{PROJECT}/schema/native.cpp", native["inputs"])
        self.assertIn(f"$(B)/{PROJECT}/schema/native.cpp.o", generated["inputs"])
        self.assertIn(native["uid"], generated["deps"])
