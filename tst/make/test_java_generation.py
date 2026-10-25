from tst.make.lib import MakeTest, PROJECT


class JavaGenerationTest(MakeTest):
    def test_java_generator(self):
        self.write("ya.make", f"JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nRUN_JAVA_PROGRAM(e2e.Generator CLASSPATH {PROJECT}/tool IN input.txt OUT Generated.java)\nJAVA_SRCS(Generated.java)\nEND()\n")
        self.write("input.txt", "42\n")
        self.write("tool/ya.make", "JAVA_PROGRAM(generator)\nJDK_VERSION(17)\nJAVA_SRCS(Generator.java)\nEND()\n")
        self.write("tool/Generator.java", 'package e2e; public class Generator { public static void main(String[] args) throws Exception { java.nio.file.Files.writeString(java.nio.file.Path.of("Generated.java"), "public class Generated {}"); } }\n')
        graph = self.graph()
        self.node(graph, "Generated.java")
        self.node(graph, "answer.jar")

    def test_generated_java_from_python(self):
        self.write("ya.make", "JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nRUN_PYTHON3(generate.py OUT Generated.java)\nJAVA_SRCS(Generated.java)\nEND()\n")
        self.write("generate.py", 'from pathlib import Path\nPath("Generated.java").write_text("public class Generated {}")\n')
        graph = self.graph()
        self.node(graph, "Generated.java")
        self.node(graph, "answer.jar")

    def test_program_script_and_jdk(self):
        self.write("ya.make", "JAVA_PROGRAM(answer)\nJDK_VERSION(17)\nJAVA_SRCS(Main.java)\nJAR_MAIN_CLASS(e2e.Main)\nWITH_JDK()\nGENERATE_SCRIPT(OUT answer.sh TEMPLATE launch.tmpl CUSTOM_PROPERTY answer 42)\nEND()\n")
        self.write("Main.java", "package e2e; public class Main { public static void main(String[] args) {} }\n")
        self.write("launch.tmpl", '#!/bin/sh\necho {{ answer }}\n')
        graph = self.graph()
        self.node(graph, "answer.sh")
        self.node(graph, "jdk.tar")

    def test_java_swig(self):
        self.write("ya.make", "DLL_JAVA(answer)\nJDK_VERSION(17)\nSRCS(answer.swg)\nEND()\n")
        self.write("answer.swg", '%module answer\n%inline %{ int answer() { return 42; } %}\n')
        graph = self.graph()
        self.node(graph, "answer.swg.jsrc")
        self.node(graph, "answer.jar")

    def test_java_flatbuffers(self):
        self.write("ya.make", f"JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nPEERDIR({PROJECT}/schema)\nJAVA_SRCS(Answer.java)\nEND()\n")
        self.write("Answer.java", "public class Answer {}\n")
        self.write("schema/ya.make", "FBS_LIBRARY()\nSRCS(answer.fbs)\nEND()\n")
        self.write("schema/answer.fbs", "namespace e2e; table Value { number:int; } root_type Value;\n")
        graph = self.graph()
        self.assertTrue(any(f"$(S)/{PROJECT}/schema/answer.fbs" in node["inputs"] and any(path.endswith((".jsrc", ".jar")) for path in node["outputs"]) for node in graph["graph"]))
