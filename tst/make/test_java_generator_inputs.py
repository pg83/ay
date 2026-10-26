import hashlib

from tst.make.lib import MakeTest, PROJECT


class JavaGeneratorInputsTest(MakeTest):
    def test_multiline_generator_arguments_keep_hash_whitespace(self):
        self.generator()
        outputs = []
        for value in ('"\n  --first=1\n  --second=2\n"', "--first=1 --second=2"):
            self.write("ya.make", f"JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nSET(FLAGS {value})\nRUN_JAVA_PROGRAM(Generator ${{FLAGS}} CLASSPATH {PROJECT}/tool OUT_DIR generated)\nJAVA_SRCS(SRCDIR ${{BINDIR}}/generated **/*.java)\nEND()\n")
            graph = self.graph()
            outputs.append(next(path for node in graph["graph"] for path in node["outputs"] if path.startswith(f"$(B)/{PROJECT}/generated.") and path.endswith(".gentar")))
            raw = f"Generator {value.strip(chr(34))} CLASSPATH {PROJECT}/tool OUT_DIR generated"
            words = [word for word in raw.replace(",", " ").split(" ") if word]
            digest = hashlib.md5(" ".join(words).encode()).hexdigest()
            self.assertEqual(outputs[-1], f"$(B)/{PROJECT}/generated.{digest}.gentar")
        self.assertNotEqual(outputs[0], outputs[1])

    def test_proto_style_resource_reaches_java(self):
        self.write("ya.make", f"JAVA_PROGRAM(answer)\nJDK_VERSION(17)\nJAVA_SRCS(Main.java)\nPEERDIR({PROJECT}/schema)\nEND()\n")
        self.write("Main.java", "public class Main {}\n")
        self.write("schema/ya.make", "PROTO_LIBRARY()\nSRCS(answer.proto)\nSTYLE_RUFF()\nEND()\n")
        self.write("schema/answer.proto", 'syntax = "proto3";\npackage e2e;\nmessage Answer {}\n')
        node = self.node(self.graph("-t", "--target-platform=default-linux-x86_64", "--target-platform-build-type=relwithdebinfo", "--host-platform=default-linux-x86_64", "--target-platform-tests", "--add-modules-to-results", "--target-platform-flag=ADD_PEERDIRS_GEN_TESTS=yes"), "test-results/java_style/meta.json")
        args = [arg for command in node["cmds"] for arg in command["cmd_args"]]
        self.assertTrue(any(arg.startswith("RUFF_RESOURCE_GLOBAL::") for arg in args))

    def test_bundled_proto_descriptor_is_a_generated_resource(self):
        self.write("ya.make", f"JAVA_PROGRAM(answer)\nJDK_VERSION(17)\nJAVA_SRCS(Main.java)\nBUNDLE({PROJECT}/descriptions NAME answer.protodesc)\nJAVA_SRCS(RESOURCES SRCDIR ${{BINDIR}} FILES answer.protodesc)\nEND()\n")
        self.write("Main.java", "public class Main {}\n")
        self.write("descriptions/ya.make", f"PROTO_DESCRIPTIONS()\nPEERDIR({PROJECT}/schema)\nEND()\n")
        self.write("schema/ya.make", "PROTO_LIBRARY()\nONLY_TAGS(DESC_PROTO)\nSRCS(answer.proto)\nEND()\n")
        self.write("schema/answer.proto", 'syntax = "proto3";\npackage e2e;\nmessage Answer {}\n')
        graph = self.graph()
        bundled = self.node(graph, "answer.protodesc")
        jar = self.node(graph, "answer.jar")
        self.assertIn(f"$(B)/{PROJECT}/answer.protodesc", jar["inputs"])
        self.assertIn(bundled["uid"], jar["deps"])

    def generator(self):
        self.write("tool/ya.make", "JAVA_PROGRAM(generator)\nJDK_VERSION(17)\nJAVA_SRCS(Generator.java)\nEND()\n")
        self.write("tool/Generator.java", "public class Generator { public static void main(String[] args) {} }\n")

    def test_source_root_input(self):
        self.generator()
        self.write("ya.make", f"JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nRUN_JAVA_PROGRAM(Generator {PROJECT}/config.txt CLASSPATH {PROJECT}/tool IN {PROJECT}/config.txt OUT Generated.java)\nJAVA_SRCS(Generated.java)\nEND()\n")
        self.write("config.txt", "42\n")
        node = self.node(self.graph(), "Generated.java")
        self.assertIn(f"$(S)/{PROJECT}/config.txt", node["inputs"])

    def test_parent_source_input_is_canonical(self):
        self.generator()
        self.write("ya.make", "RECURSE(client)\n")
        self.write("config.txt", "42\n")
        for input_path in ("${CURDIR}/../config.txt", "../config.txt"):
            with self.subTest(input_path=input_path):
                self.write("client/ya.make", f"JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nRUN_JAVA_PROGRAM(Generator {input_path} CLASSPATH {PROJECT}/tool IN {input_path} ${{ARCADIA_ROOT}}/{PROJECT}/config.txt OUT Generated.java)\nJAVA_SRCS(Generated.java)\nEND()\n")
                node = self.node(self.graph(), "client/Generated.java")
                inputs = [path for path in node["inputs"] if path.endswith("/config.txt")]
                self.assertEqual(inputs, [f"$(S)/{PROJECT}/config.txt"])

    def test_run_program_output_is_an_input(self):
        self.generator()
        self.write("ya.make", f"JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nRUN_PROGRAM({PROJECT}/native OUT generated.txt)\nRUN_JAVA_PROGRAM(Generator generated.txt CLASSPATH {PROJECT}/tool IN generated.txt OUT Generated.java)\nJAVA_SRCS(Generated.java)\nEND()\n")
        self.write("native/ya.make", "PY3_PROGRAM(native)\nPY_SRCS(MAIN main.py)\nEND()\n")
        self.write("native/main.py", "print(42)\n")
        graph = self.graph()
        generated = self.node(graph, "generated.txt")
        java = self.node(graph, "Generated.java")
        self.assertIn(f"$(B)/{PROJECT}/generated.txt", java["inputs"])
        self.assertIn(generated["uid"], java["deps"])

    def test_native_tool_argument(self):
        self.generator()
        self.write("ya.make", f"JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nRUN_JAVA_PROGRAM(Generator tool:{PROJECT}/native CLASSPATH {PROJECT}/tool TOOL {PROJECT}/native OUT Generated.java)\nJAVA_SRCS(Generated.java)\nEND()\n")
        self.write("native/ya.make", "PROGRAM(native)\nSRCS(main.cpp)\nEND()\n")
        self.write("native/main.cpp", "int main() { return 0; }\n")
        node = self.node(self.graph(), "Generated.java")
        self.assertIn(f"$(B)/{PROJECT}/native/native", node["inputs"])
        args = [arg.replace("$(BUILD_ROOT)", "$(B)") for command in node["cmds"] for arg in command["cmd_args"]]
        self.assertIn(f"tool:$(B)/{PROJECT}/native/native", args)

