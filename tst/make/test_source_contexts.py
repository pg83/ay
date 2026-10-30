from tst.make.lib import MakeTest, PROJECT


class SourceContextsTest(MakeTest):
    def test_declared_text_filenames_preserve_embedded_dollar_signs(self):
        self.write("ya.make", '''LIBRARY()
SET(E2E_LEAF expanded)
DECLARE_IN_DIRS(DATA *.h DIRS data)
RUN_PYTHON3(generate.py IN ${DATA_FILES} STDOUT generated.cpp)
END()
''')
        self.write("generate.py", 'print("int answer() { return 42; }")\n')
        self.write("data/$E2E_LEAF.h", "#define LITERAL 1\n")
        self.write("data/expanded.h", "#define EXPANDED 2\n")
        inputs = self.node(self.graph(), "generated.cpp")["inputs"]
        for name in ("$E2E_LEAF.h", "expanded.h"):
            self.assertIn(f"$(S)/{PROJECT}/data/{name}", inputs)

    def test_late_bound_program_inputs_preserve_declared_text_files(self):
        self.write("ya.make", f'''LIBRARY()
RUN_PROGRAM({PROJECT}/generator IN $E2E_LATE ordinary.h OUT generated.cpp)
DECLARE_IN_DIRS(DATA *.h DIRS data)
SET(E2E_LATE ${{DATA_FILES}})
END()
''')
        self.write("generator/ya.make", "PY3_PROGRAM(generator)\nPY_SRCS(MAIN main.py)\nEND()\n")
        self.write("generator/main.py", 'from pathlib import Path\nPath("generated.cpp").write_text("int answer() { return 42; }")\n')
        self.write("data/value.h", '#include "../unparsed.h"\n')
        self.write("unparsed.h", "#define UNPARSED 1\n")
        self.write("ordinary.h", '#include "parsed.h"\n')
        self.write("parsed.h", "#define PARSED 1\n")
        inputs = self.node(self.graph(), "generated.cpp")["inputs"]
        for name in ("data/value.h", "ordinary.h", "parsed.h"):
            self.assertIn(f"$(S)/{PROJECT}/{name}", inputs)
        self.assertNotIn(f"$(S)/{PROJECT}/unparsed.h", inputs)

    def test_literal_and_declared_text_inputs_keep_separate_scan_policies(self):
        self.write("ya.make", '''LIBRARY()
DECLARE_IN_DIRS(DATA *.h DIRS data)
SET(INPUTS ${DATA_FILES})
SET_APPEND(INPUTS extra.h)
RUN_PYTHON3(generate.py IN ordinary.h ${INPUTS} STDOUT generated.cpp)
END()
''')
        self.write("generate.py", 'print("int answer() { return 42; }")\n')
        self.write("data/value.h", '#include "../unparsed.h"\n')
        self.write("unparsed.h", "#define UNPARSED 1\n")
        self.write("ordinary.h", '#include "parsed.h"\n')
        self.write("extra.h", '#include "parsed.h"\n')
        self.write("parsed.h", "#define PARSED 1\n")
        inputs = self.node(self.graph(), "generated.cpp")["inputs"]
        for name in ("data/value.h", "ordinary.h", "extra.h", "parsed.h"):
            self.assertIn(f"$(S)/{PROJECT}/{name}", inputs)
        self.assertNotIn(f"$(S)/{PROJECT}/unparsed.h", inputs)

    def test_directory_declarations_exclude_matching_file_names(self):
        self.write("ya.make", "LIBRARY()\nDECLARE_IN_DIRS(DATA *.txt DIRS . EXCLUDES ignored.txt)\nRUN_PYTHON3(generate.py IN ${DATA_FILES} STDOUT generated.cpp)\nEND()\n")
        self.write("generate.py", 'print("int answer() { return 42; }")\n')
        for name in ("ignored", "retained"):
            self.write(name + ".txt", "42\n")
        inputs = self.node(self.graph(), "generated.cpp")["inputs"]
        self.assertIn(f"$(S)/{PROJECT}/retained.txt", inputs)
        self.assertNotIn(f"$(S)/{PROJECT}/ignored.txt", inputs)

    def test_directory_declarations_exclude_matching_relative_paths(self):
        self.write("ya.make", f"LIBRARY()\nDECLARE_IN_DIRS(DATA *.txt SRCDIR ${{ARCADIA_ROOT}} RECURSIVE DIRS {PROJECT}/data EXCLUDES {PROJECT}/data/ignored/*.txt)\nRUN_PYTHON3(generate.py IN ${{DATA_FILES}} STDOUT generated.cpp)\nEND()\n")
        self.write("generate.py", 'print("int answer() { return 42; }")\n')
        for name in ("ignored/answer", "retained"):
            self.write("data/" + name + ".txt", "42\n")
        inputs = self.node(self.graph(), "generated.cpp")["inputs"]
        self.assertIn(f"$(S)/{PROJECT}/data/retained.txt", inputs)
        self.assertNotIn(f"$(S)/{PROJECT}/data/ignored/answer.txt", inputs)

    def test_escaped_scalar_and_text_input_context(self):
        self.write("ya.make", '''LIBRARY()
DECLARE_IN_DIRS(DATA *.h DIRS data)
SET(ESCAPED un\\_related)
RUN_PYTHON3(generate.py ${ESCAPED} IN ${DATA_FILES} STDOUT generated.cpp)
END()
''')
        self.write("generate.py", 'print("int answer() { return 42; }")\n')
        self.write("data/value.h", '#include "../hidden.h"\n')
        self.write("hidden.h", "#define HIDDEN 1\n")
        node = self.node(self.graph(), "generated.cpp")
        inputs = node["inputs"]
        self.assertIn(f"$(S)/{PROJECT}/data/value.h", inputs)
        self.assertNotIn(f"$(S)/{PROJECT}/hidden.h", inputs)
        self.assertIn("un_related", [arg for command in node["cmds"] for arg in command["cmd_args"]])

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
        compiled = self.node(graph, "value.cpp.o")
        generated = self.node(graph, "answer.fbs.h")
        self.assertIn(f"$(S)/{PROJECT}/value.cpp", compiled["inputs"])
        self.assertIn(f"$(B)/{PROJECT}/answer.fbs.h", compiled["inputs"])
        self.assertIn(generated["uid"], compiled["deps"])

    def test_native_java_generator(self):
        self.write("ya.make", f"LIBRARY()\nJDK_VERSION(17)\nRUN_JAVA_PROGRAM(Generator CLASSPATH {PROJECT}/generator OUT answer.cpp)\nEND()\n")
        self.write("generator/ya.make", "JAVA_PROGRAM(generator)\nJDK_VERSION(17)\nJAVA_SRCS(Generator.java)\nEND()\n")
        self.write("generator/Generator.java", 'public class Generator { public static void main(String[] args) throws Exception { java.nio.file.Files.writeString(java.nio.file.Path.of("answer.cpp"), "int answer() { return 42; }"); } }\n')
        self.node(self.graph(), "answer.cpp.o")
