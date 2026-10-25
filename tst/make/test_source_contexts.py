from tst.make.lib import MakeTest, PROJECT


class SourceContextsTest(MakeTest):
    def test_text_inputs_through_variables(self):
        self.write("ya.make", f'''LIBRARY()
DECLARE_IN_DIRS(DATA *.txt DIRS data)
DECLARE_IN_DIRS(OTHER *.txt DIRS other)
SET(INPUTS ${{DATA_FILES}})
SET_APPEND(INPUTS ${{OTHER_FILES}})
SET(ALL_INPUTS ${{INPUTS}} ordinary.txt)
RUN_PYTHON3(generate.py IN ${{ALL_INPUTS}} STDOUT generated.cpp)
END()
''')
        self.write("generate.py", 'print("int answer() { return 42; }")\n')
        for name in ("data/value", "other/value", "ordinary"):
            self.write(name + ".txt", "42\n")
        inputs = self.node(self.graph(), "generated.cpp")["inputs"]
        for name in ("data/value", "other/value", "ordinary"):
            self.assertIn(f"$(S)/{PROJECT}/{name}.txt", inputs)

    def test_flatbuffers_native_context(self):
        self.write("ya.make", f'FBS_LIBRARY()\nSRCS(answer.fbs)\nIF (MODULE_TAG == "CPP_FBS")\nADDINCL(${{ARCADIA_BUILD_ROOT}}/{PROJECT})\nSRCS(value.cpp)\nENDIF()\nEND()\n')
        self.write("answer.fbs", "namespace e2e; table Answer { value:int; } root_type Answer;\n")
        self.write("value.cpp", '#include "answer.fbs.h"\nint answer() { return 42; }\n')
        graph = self.graph()
        self.assertTrue(any(f"$(S)/{PROJECT}/value.cpp" in node["inputs"] for node in graph["graph"]))

    def test_native_java_generator(self):
        self.write("ya.make", f"LIBRARY()\nJDK_VERSION(17)\nRUN_JAVA_PROGRAM(Generator CLASSPATH {PROJECT}/generator OUT answer.cpp)\nEND()\n")
        self.write("generator/ya.make", "JAVA_PROGRAM(generator)\nJDK_VERSION(17)\nJAVA_SRCS(Generator.java)\nEND()\n")
        self.write("generator/Generator.java", 'public class Generator { public static void main(String[] args) throws Exception { java.nio.file.Files.writeString(java.nio.file.Path.of("answer.cpp"), "int answer() { return 42; }"); } }\n')
        self.node(self.graph(), "answer.cpp.o")
