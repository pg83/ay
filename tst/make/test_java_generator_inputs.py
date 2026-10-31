import hashlib
import re

from tst.make.lib import MakeTest, PROJECT


class JavaGeneratorInputsTest(MakeTest):
    def test_input_jar_keeps_native_inputs_from_an_outputless_contrib_proxy(self):
        self.generator()
        self.write("ya.make", f"JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nPEERDIR({PROJECT}/subject)\nRUN_JAVA_PROGRAM(Generator CLASSPATH {PROJECT}/tool IN ${{ARCADIA_BUILD_ROOT}}/{PROJECT}/subject/subject.jar OUT Generated.java)\nJAVA_SRCS(Generated.java)\nEND()\n")
        self.write("subject/ya.make", f"JAVA_LIBRARY(subject)\nJDK_VERSION(17)\nPEERDIR({PROJECT}/proxy)\nEND()\n")
        self.write("proxy/ya.make", f"JAVA_CONTRIB_PROXY(proxy)\nPEERDIR({PROJECT}/native)\nEND()\n")
        self.write("native/ya.make", "LIBRARY(native)\nSRCS(answer.cpp)\nEND()\n")
        self.write("native/answer.cpp", "int answer() { return 42; }\n")
        graph = self.graph("-DSBOM_GENERATION_ALLOWED=no")
        generated = self.node(graph, "Generated.java")
        self.assertIn(f"$(B)/{PROJECT}/native/libnative.a", generated["inputs"])
        self.assertIn(self.node(graph, "native/libnative.a")["uid"], generated["deps"])

    def test_java_generator_requires_a_runnable_host_variant(self):
        self.generator()
        self.write("ya.make", f"JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nRUN_JAVA_PROGRAM(Generator CLASSPATH {PROJECT}/tool OUT Generated.java)\nJAVA_SRCS(Generated.java)\nEND()\n")
        self.write("tool/ya.make", "IF(OS_WINDOWS)\nJAVA_PROGRAM(generator)\nELSE()\nJAVA_LIBRARY(generator)\nENDIF()\nJDK_VERSION(17)\nJAVA_SRCS(Generator.java)\nEND()\n")
        result = self.make("--target-platform=default-win-x86_64")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn(f"{PROJECT}/tool", result.stderr)

    def test_input_jar_keeps_native_archives_and_resources(self):
        self.generator()
        self.write("ya.make", f"JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nPEERDIR({PROJECT}/subject)\nRUN_JAVA_PROGRAM(Generator CLASSPATH {PROJECT}/tool IN ${{ARCADIA_BUILD_ROOT}}/{PROJECT}/subject/subject.jar OUT Generated.java)\nJAVA_SRCS(Generated.java)\nEND()\n")
        self.write("subject/ya.make", f"JAVA_LIBRARY(subject)\nJDK_VERSION(17)\nPEERDIR({PROJECT}/native)\nEND()\n")
        self.write("native/ya.make", f"LIBRARY(native)\nSRCS(answer.cpp)\nRESOURCE(value.txt /value)\nPEERDIR({PROJECT}/leaf)\nEND()\n")
        self.write("native/answer.cpp", "int answer() { return 42; }\n")
        self.write("native/value.txt", "42\n")
        self.write("leaf/ya.make", "LIBRARY(leaf)\nSRCS(leaf.cpp)\nEND()\n")
        self.write("leaf/leaf.cpp", "int leaf() { return 17; }\n")
        graph = self.graph()
        node = self.node(graph, "Generated.java")
        for output in ("native/libnative.a", "native/libnative.global.a", "leaf/libleaf.a"):
            with self.subTest(output=output):
                path = f"$(B)/{PROJECT}/{output}"
                self.assertIn(path, node["inputs"])
                self.assertTrue(any(path in peer["outputs"] and peer["uid"] in node["deps"] for peer in graph["graph"]))

    def test_generator_preserves_literal_dollars_and_expands_references(self):
        self.generator()
        self.write("ya.make", f'''JAVA_LIBRARY(answer)
JDK_VERSION(17)
SET(E2E_VALUE expanded)
RUN_JAVA_PROGRAM(Generator $ $9 $- $E2E_MISSING ${{E2E_MISSING}} $E2E_VALUE ${{E2E_VALUE}} CLASSPATH {PROJECT}/tool OUT Generated.java)
JAVA_SRCS(Generated.java)
END()
''')
        node = self.node(self.graph(), "Generated.java")
        args = next(command["cmd_args"] for command in node["cmds"] if "Generator" in command["cmd_args"])
        start = args.index("Generator")
        self.assertEqual(args[start + 1:], ["$", "$9", "$-", "expanded", "expanded"])

    def test_malformed_generator_variable_references(self):
        self.generator()
        for reference in ("${}", "${broken"):
            with self.subTest(reference=reference):
                self.write("ya.make", f"JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nRUN_JAVA_PROGRAM(Generator {reference} CLASSPATH {PROJECT}/tool OUT Generated.java)\nJAVA_SRCS(Generated.java)\nEND()\n")
                self.assertNotEqual(self.make().returncode, 0)

    def test_recursive_generator_variable_is_an_error(self):
        self.generator()
        self.write("ya.make", f'''JAVA_LIBRARY(answer)
JDK_VERSION(17)
SET(E2E_RECURSIVE $E2E_RECURSIVE)
RUN_JAVA_PROGRAM(Generator $E2E_RECURSIVE CLASSPATH {PROJECT}/tool OUT Generated.java)
JAVA_SRCS(Generated.java)
END()
''')
        self.assertNotEqual(self.make().returncode, 0)

    def test_input_jar_keeps_peer_sbom(self):
        self.generator()
        self.write("ya.make", f"JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nPEERDIR({PROJECT}/subject)\nRUN_JAVA_PROGRAM(Generator CLASSPATH {PROJECT}/tool IN ${{ARCADIA_BUILD_ROOT}}/{PROJECT}/subject/subject.jar OUT Generated.java)\nJAVA_SRCS(Generated.java)\nEND()\n")
        self.write("subject/ya.make", "JAVA_LIBRARY(subject)\nJDK_VERSION(17)\nJAVA_SRCS(Subject.java)\nEND()\n")
        self.write("subject/Subject.java", "public class Subject {}\n")
        graph = self.graph()
        node = self.node(graph, "Generated.java")
        output = "$(B)/build/platform/java/jdk/jdk17/toolchain.component.sbom"
        self.assertIn(output, node["inputs"])
        producers = {item["uid"] for item in graph["graph"] if output in item["outputs"]}
        self.assertTrue(producers.intersection(node["deps"]))

    def test_input_jar_keeps_managed_transitive_version(self):
        commons = "contrib/java/org/apache/commons/commons-lang3"
        self.generator()
        self.write("ya.make", f"JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nPEERDIR({PROJECT}/subject)\nRUN_JAVA_PROGRAM(Generator CLASSPATH {PROJECT}/tool IN ${{ARCADIA_BUILD_ROOT}}/{PROJECT}/subject/subject.jar OUT Generated.java)\nJAVA_SRCS(Generated.java)\nEND()\n")
        self.write("subject/ya.make", f"JAVA_PROGRAM(subject)\nJDK_VERSION(17)\nJAVA_SRCS(Subject.java)\nPEERDIR({PROJECT}/lib)\nDEPENDENCY_MANAGEMENT({commons}/3.12.0)\nEND()\n")
        self.write("subject/Subject.java", "public class Subject {}\n")
        self.write("lib/ya.make", f"JAVA_LIBRARY(lib)\nJDK_VERSION(17)\nJAVA_SRCS(Lib.java)\nPEERDIR({commons}/3.10)\nEND()\n")
        self.write("lib/Lib.java", "public class Lib {}\n")
        node = self.node(self.graph(), "Generated.java")
        for version in ("3.10", "3.12.0"):
            self.assertIn(f"$(B)/{commons}/{version}/commons-lang3-{version}.jar", node["inputs"])

    def test_input_jar_keeps_native_peer_closure(self):
        self.generator()
        self.write("ya.make", f"JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nPEERDIR({PROJECT}/subject)\nRUN_JAVA_PROGRAM(Generator CLASSPATH {PROJECT}/tool IN ${{ARCADIA_BUILD_ROOT}}/{PROJECT}/subject/subject.jar OUT Generated.java)\nJAVA_SRCS(Generated.java)\nEND()\n")
        self.write("subject/ya.make", f"JAVA_PROGRAM(subject)\nJDK_VERSION(17)\nJAVA_SRCS(Subject.java)\nPEERDIR({PROJECT}/binding)\nEND()\n")
        self.write("subject/Subject.java", "public class Subject {}\n")
        self.write("binding/ya.make", "DLL_JAVA(binding)\nJDK_VERSION(17)\nSRCS(binding.swg)\nEND()\n")
        self.write("binding/binding.swg", '%module binding\n%inline %{ int answer() { return 42; } %}\n')
        graph = self.graph()
        node = self.node(graph, "Generated.java")
        self.assertIn("$(B)/util/libyutil.a", node["inputs"])
        self.assertFalse([path for path in node["inputs"] if path.endswith(".pkg.fake")])
        self.assertNotIn("$(B)/contrib/libs/jdk/libcontrib-libs-jdk.a", node["inputs"])
        for path in ("$(B)/util/libyutil.a", f"$(B)/{PROJECT}/binding/libbinding.so"):
            with self.subTest(dependency=path):
                producers = {item["uid"] for item in graph["graph"] if path in item["outputs"]}
                self.assertTrue(producers.intersection(node["deps"]), path)

    def test_input_java_manifest_omits_fake_modules(self):
        self.generator()
        self.write("ya.make", f"JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nPEERDIR({PROJECT}/subject)\nRUN_JAVA_PROGRAM(Generator CLASSPATH {PROJECT}/tool IN ${{ARCADIA_BUILD_ROOT}}/{PROJECT}/subject/subject.run.cp.jar OUT Generated.java)\nJAVA_SRCS(Generated.java)\nEND()\nRECURSE(subject)\n")
        self.write("subject/ya.make", "JAVA_PROGRAM(subject)\nJDK_VERSION(17)\nJAVA_SRCS(Subject.java)\nEND()\n")
        self.write("subject/Subject.java", "public class Subject {}\n")
        node = self.node(self.graph(), "Generated.java")
        self.assertIn(f"$(B)/{PROJECT}/subject/subject.jar", node["inputs"])
        self.assertFalse([path for path in node["inputs"] if path.endswith(".pkg.fake")])

    def test_dotted_variable_in_generator_archive_hash(self):
        self.generator()
        self.write("ya.make", f"JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nSET(generator.version 42)\nRUN_JAVA_PROGRAM(Generator ${{generator.version}} CLASSPATH {PROJECT}/tool OUT_DIR generated)\nJAVA_SRCS(SRCDIR ${{BINDIR}}/generated **/*.java)\nEND()\n")
        raw = f"Generator 42 CLASSPATH {PROJECT}/tool OUT_DIR generated"
        digest = hashlib.md5(raw.encode()).hexdigest()
        self.node(self.graph(), f"generated.{digest}.gentar")

    def test_input_jar_keeps_transitive_classpath(self):
        self.generator()
        self.write("ya.make", f"JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nPEERDIR({PROJECT}/subject)\nRUN_JAVA_PROGRAM(Generator ${{ARCADIA_BUILD_ROOT}}/{PROJECT}/subject/subject.jar CLASSPATH {PROJECT}/tool IN ${{ARCADIA_BUILD_ROOT}}/{PROJECT}/subject/subject.jar OUT Generated.java)\nJAVA_SRCS(Generated.java)\nEND()\n")
        self.write("subject/ya.make", f"JAVA_LIBRARY(subject)\nJDK_VERSION(17)\nJAVA_SRCS(Subject.java)\nPEERDIR({PROJECT}/leaf)\nEND()\n")
        self.write("subject/Subject.java", "public class Subject {}\n")
        self.write("leaf/ya.make", "JAVA_LIBRARY(leaf)\nJDK_VERSION(17)\nJAVA_SRCS(Leaf.java)\nEND()\n")
        self.write("leaf/Leaf.java", "public class Leaf {}\n")
        node = self.node(self.graph(), "Generated.java")
        for name in ("subject/subject.jar", "leaf/leaf.jar"):
            self.assertIn(f"$(B)/{PROJECT}/{name}", node["inputs"])

    def test_proto_input_keeps_imports(self):
        self.generator()
        self.write("data/entry.proto", f'syntax = "proto3";\nimport "{PROJECT}/schema/value.proto";\nmessage Entry {{ Value value = 1; }}\n')
        self.write("schema/value.proto", 'syntax = "proto3";\nmessage Value { int32 answer = 1; }\n')
        for inputs in ("IN data/entry.proto", "IN_NOPARSE data/entry.proto", "IN_DIR data"):
            with self.subTest(inputs=inputs):
                self.write("ya.make", f"JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nRUN_JAVA_PROGRAM(Generator CLASSPATH {PROJECT}/tool {inputs} OUT Generated.java)\nJAVA_SRCS(Generated.java)\nEND()\n")
                node = self.node(self.graph(), "Generated.java")
                self.assertEqual(f"$(S)/{PROJECT}/schema/value.proto" in node["inputs"], inputs.startswith("IN "))

    def test_proto_input_keeps_system_imports(self):
        self.generator()
        self.write("ya.make", f"JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nRUN_JAVA_PROGRAM(Generator CLASSPATH {PROJECT}/tool IN entry.proto OUT Generated.java)\nJAVA_SRCS(Generated.java)\nEND()\n")
        self.write("entry.proto", 'syntax = "proto3";\nimport "google/protobuf/wrappers.proto";\nmessage Entry { google.protobuf.Int32Value value = 1; }\n')
        node = self.node(self.graph(), "Generated.java")
        for root in ("protobuf", "protobuf_old", "protobuf_std"):
            self.assertIn(f"$(S)/contrib/libs/{root}/src/google/protobuf/wrappers.proto", node["inputs"])

    def test_native_module_input_keeps_includes(self):
        self.generator()
        self.write("entry.h", '#include "local.h"\n')
        self.write("local.h", "int answer();\n")
        for inputs in ("IN entry.h", "IN_NOPARSE entry.h"):
            with self.subTest(inputs=inputs):
                self.write("ya.make", f"LIBRARY()\nRUN_JAVA_PROGRAM(Generator CLASSPATH {PROJECT}/tool {inputs} OUT generated.cpp)\nEND()\n")
                node = self.node(self.graph(), "generated.cpp")
                self.assertEqual(f"$(S)/{PROJECT}/local.h" in node["inputs"], inputs.startswith("IN "))

    def test_proto_cpp_style_resource_reaches_java(self):
        self.write("ya.make", f"EXECTEST()\nDEPENDS({PROJECT}/program)\nRUN(${{ARCADIA_BUILD_ROOT}}/{PROJECT}/program/run.sh)\nEND()\nRECURSE(program)\n")
        self.write("program/ya.make", f"JAVA_PROGRAM(answer)\nJDK_VERSION(17)\nJAVA_SRCS(Main.java)\nPEERDIR({PROJECT}/schema)\nEND()\n")
        self.write("program/Main.java", "public class Main {}\n")
        self.write("schema/ya.make", "PROTO_LIBRARY()\nSRCS(answer.proto)\nGENERATE_ENUM_SERIALIZATION(answer.pb.h)\nSTYLE_CPP()\nEND()\n")
        self.write("schema/answer.proto", 'syntax = "proto3";\npackage e2e;\nenum Answer { UNKNOWN = 0; VALUE = 42; }\n')
        graph = self.graph("-t", "--target-platform=default-linux-x86_64", "--target-platform-build-type=relwithdebinfo", "--host-platform=default-linux-x86_64", "--target-platform-tests", "--add-modules-to-results", "--target-platform-flag=ADD_PEERDIRS_GEN_TESTS=yes")
        with self.subTest(node="style"):
            node = self.node(graph, "program/test-results/java_style/meta.json")
            args = [arg for command in node["cmds"] for arg in command["cmd_args"]]
            self.assertTrue(any(arg.startswith("CLANG_FORMAT_RESOURCE_GLOBAL::") for arg in args))
        with self.subTest(node="program"):
            node = self.node(graph, "program/answer.run.cp.jar")
            args = [arg for command in node["cmds"] for arg in command["cmd_args"]]
            self.assertTrue(any("::platform-clang-clang-format.pkg.fake" in arg for arg in args))

    def test_proto_cpp_style_check_is_one_suite_per_directory(self):
        self.write("program/ya.make", f"JAVA_PROGRAM(answer)\nJDK_VERSION(17)\nJAVA_SRCS(Main.java)\nPEERDIR({PROJECT}/schema)\nEND()\n")
        self.write("program/Main.java", "public class Main {}\n")
        self.write("native/ya.make", f"PROGRAM(answer)\nSRCS(main.cpp)\nPEERDIR({PROJECT}/schema)\nEND()\n")
        self.write("native/main.cpp", "int main() { return 0; }\n")
        self.write("schema/ya.make", "PROTO_LIBRARY()\nSRCS(answer.proto)\nSTYLE_CPP()\nEND()\n")
        self.write("schema/answer.proto", 'syntax = "proto3";\npackage e2e;\nmessage Answer {}\n')
        self.write("schema/answer.h", "#pragma once\n")
        for programs in (["program"], ["program", "native"]):
            with self.subTest(programs=programs):
                self.write("ya.make", "EXECTEST()\n" + "".join(f"DEPENDS({PROJECT}/{p})\n" for p in programs) + f"RUN(${{ARCADIA_BUILD_ROOT}}/{PROJECT}/program/run.sh)\nEND()\nRECURSE({' '.join(programs)})\n")
                graph = self.graph("-t", "--target-platform=default-linux-x86_64", "--target-platform-build-type=relwithdebinfo", "--host-platform=default-linux-x86_64", "--target-platform-tests", "--add-modules-to-results", "--target-platform-flag=ADD_PEERDIRS_GEN_TESTS=yes")
                node = self.node(graph, "schema/test-results/clang_format/meta.json")
                args = [arg.replace("$(SOURCE_ROOT)", "$(S)") for command in node["cmds"] for arg in command["cmd_args"]]
                self.assertIn(f"$(S)/{PROJECT}/schema/answer.h", args)

    def proto_style_classpath(self, body):
        self.write("ya.make", f"EXECTEST()\nDEPENDS({PROJECT}/program)\nRUN(${{ARCADIA_BUILD_ROOT}}/{PROJECT}/program/run.sh)\nEND()\nRECURSE(program)\n")
        self.write("program/ya.make", f"JAVA_PROGRAM(answer)\nJDK_VERSION(17)\nJAVA_SRCS(Main.java)\nPEERDIR({PROJECT}/schema)\nEND()\n")
        self.write("program/Main.java", "public class Main {}\n")
        self.write("schema/ya.make", f"PROTO_LIBRARY()\n{body}END()\n")
        self.write("schema/answer.proto", 'syntax = "proto3";\npackage e2e;\nenum Answer { UNKNOWN = 0; VALUE = 42; }\n')
        node = self.node(self.graph("-t"), "program/answer.run.cp.jar")
        return next(arg for command in node["cmds"] for arg in command["cmd_args"] if arg.startswith("CLASSPATH=")).split("::")

    def test_proto_cpp_style_peer_precedes_proto_tool(self):
        classpath = self.proto_style_classpath("SRCS(answer.proto)\nSTYLE_CPP()\n")
        self.assertLess(classpath.index("platform-clang-clang-format.pkg.fake"), classpath.index("platform-java-protoc.pkg.fake"))

    def test_proto_python_linter_peer_follows_module_peers(self):
        peers = ["platform-clang-clang-format.pkg.fake", "platform-java-protoc.pkg.fake", "build-external_resources-black.pkg.fake"]
        for body in ("STYLE_PYTHON()\nSRCS(answer.proto)\nSTYLE_CPP()\n", "STYLE_CPP()\nSRCS(answer.proto)\nSTYLE_PYTHON()\n"):
            with self.subTest(body=body):
                classpath = self.proto_style_classpath(body)
                self.assertEqual(sorted(peers, key=classpath.index), peers)

    def test_proto_python_linter_peer_obeys_no_lint(self):
        for body, linted in (("NO_LINT()\nSRCS(answer.proto)\nSTYLE_PYTHON()\n", False), ("NO_LINT(ktlint)\nSRCS(answer.proto)\nSTYLE_PYTHON()\n", True)):
            with self.subTest(body=body):
                self.assertEqual("build-external_resources-black.pkg.fake" in self.proto_style_classpath(body), linted)

    def test_proto_linter_peers_follow_statement_priority(self):
        self.write("ya.make", f"JAVA_PROGRAM(answer)\nJDK_VERSION(17)\nJAVA_SRCS(Main.kt)\nWITH_KOTLIN()\nPEERDIR({PROJECT}/schema)\nEND()\n")
        self.write("Main.kt", "fun main() {}\n")
        self.write("schema/answer.proto", 'syntax = "proto3";\npackage e2e;\nmessage Answer {}\n')
        for styles in ("STYLE_CPP()\nSTYLE_PYTHON()", "STYLE_PYTHON()\nSTYLE_CPP()", "STYLE_RUFF()\nSTYLE_CPP()"):
            with self.subTest(styles=styles):
                self.write("schema/linters.inc", styles + "\n")
                self.write("schema/ya.make", f"PROTO_LIBRARY()\nSRCS(answer.proto)\nINCLUDE(${{ARCADIA_ROOT}}/{PROJECT}/schema/linters.inc)\nEND()\n")
                graph = self.graph("-t")
                node = self.node(graph, "answer.run.cp.jar")
                classpath = next(arg for command in node["cmds"] for arg in command["cmd_args"] if arg.startswith("CLASSPATH=")).split("::")
                python = "ruff" if "STYLE_RUFF" in styles else "black"
                peers = ["platform-clang-clang-format.pkg.fake", "platform-java-protoc.pkg.fake", f"build-external_resources-{python}.pkg.fake"]
                self.assertEqual([entry for entry in classpath if entry in peers], peers)
                ktlint = self.node(graph, "test-results/ktlint/meta.json")
                self.assertIn(node["uid"], ktlint["deps"])

    def test_tasklet_peer_keeps_macro_order(self):
        self.write("ya.make", f"JAVA_PROGRAM(answer)\nJDK_VERSION(17)\nJAVA_SRCS(Main.java)\nPEERDIR({PROJECT}/schema)\nEND()\n")
        self.write("Main.java", "public class Main {}\n")
        self.write("schema/ya.make", f"PROTO_SCHEMA()\nTASKLET()\nEXCLUDE_TAGS(CPP_PROTO_FROM_SCHEMA)\nPEERDIR({PROJECT}/leaf tasklet/v1/api)\nSRCS(answer.proto)\nEND()\n")
        self.write("schema/answer.proto", 'syntax = "proto3";\npackage e2e;\nmessage Answer {}\n')
        self.write("leaf/ya.make", "PROTO_SCHEMA()\nSRCS(value.proto)\nEND()\n")
        self.write("leaf/value.proto", 'syntax = "proto3";\npackage e2e;\nmessage Value {}\n')
        graph = self.graph()
        jars = [node for node in graph["graph"] if any(out.startswith(f"$(B)/{PROJECT}/schema/") and out.endswith(".jar") for out in node["outputs"])]
        self.assertEqual(len(jars), 1)
        args = [arg.replace("$(BUILD_ROOT)", "$(B)") for command in jars[0]["cmds"] for arg in command["cmd_args"]]
        tasklet = args.index("$(B)/tasklet/v1/api/tasklet-v1-api.jar")
        leaf = next(i for i, arg in enumerate(args) if arg.startswith(f"$(B)/{PROJECT}/leaf/") and arg.endswith(".jar"))
        self.assertLess(tasklet, leaf)

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

    def test_proto_yaml_style_resource_reaches_java(self):
        self.write("ya.make", f"JAVA_PROGRAM(answer)\nJDK_VERSION(17)\nJAVA_SRCS(Main.java)\nPEERDIR({PROJECT}/schema)\nEND()\n")
        self.write("Main.java", "public class Main {}\n")
        self.write("schema/ya.make", "PROTO_LIBRARY()\nSRCS(answer.proto)\nSTYLE_YAML_WITH_CONFIG()\nEND()\n")
        self.write("schema/answer.proto", 'syntax = "proto3";\npackage e2e;\nmessage Answer {}\n')
        node = self.node(self.graph("-t", "--target-platform=default-linux-x86_64", "--target-platform-build-type=relwithdebinfo", "--host-platform=default-linux-x86_64", "--target-platform-tests", "--add-modules-to-results", "--target-platform-flag=ADD_PEERDIRS_GEN_TESTS=yes"), "test-results/java_style/meta.json")
        args = [arg for command in node["cmds"] for arg in command["cmd_args"]]
        self.assertTrue(any(arg.startswith("YAMLFMT_RESOURCE_GLOBAL::") for arg in args))

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

    def test_tool_classpath_depends_on_the_tool_only(self):
        # The tool is also a target here (RECURSE, configured before the
        # client) and the target platform is configured apart from the host:
        # the target build writes the same .run.cp.jar, but the command uses
        # the host tool alone.
        self.generator()
        self.write("ya.make", "RECURSE(tool client)\n")
        self.write("client/ya.make", f"JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nRUN_JAVA_PROGRAM(Generator CLASSPATH {PROJECT}/tool OUT Generated.java)\nJAVA_SRCS(Generated.java)\nEND()\n")
        graph = self.graph("--target-platform", "default-linux-x86_64", "--target-platform-build-type", "relwithdebinfo")
        node = self.node(graph, "client/Generated.java")
        classpath = f"$(B)/{PROJECT}/tool/generator.run.cp.jar"
        self.assertIn(classpath, node["inputs"])
        producers = {producer["uid"] for producer in graph["graph"] if classpath in producer["outputs"]}
        self.assertEqual(len(producers & set(node["deps"])), 1)

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


    def test_docs_generator_runs_default_jdk_on_written_paths(self):
        # The DOCS library runs the generator: DEFAULT_JDK_VERSION picks the
        # JDK resource for $JDK_RESOURCE, and the arguments keep the paths
        # exactly as written (the double slash of a directory variable).
        self.write("tool/ya.make", "JAVA_PROGRAM(generator)\nDEFAULT_JDK_VERSION(21)\nWITH_JDK()\nJAVA_SRCS(Generator.java)\nEND()\n")
        self.write("tool/Generator.java", "public class Generator { public static void main(String[] args) {} }\n")
        self.write("ya.make", f"DOCS()\nSET(RESULT ${{ARCADIA_BUILD_ROOT}}/{PROJECT}/)\nDEFAULT_JDK_VERSION(21)\nRUN_JAVA_PROGRAM(Generator ${{RESULT}}/nested/answer.yaml OUT_NOAUTO ${{RESULT}}/nested/answer.yaml CLASSPATH {PROJECT}/tool)\nEND()\n")
        self.write(".yfm", "strict: true\n")
        self.write("toc.yaml", "title: Answer\nitems:\n  - name: Answer\n    href: index.md\n")
        self.write("index.md", "# Answer\n")
        node = self.node(self.graph(), "nested/answer.yaml")
        # ya spells a resource root $(NAME-ID), ay $(B)/resources/NAME.
        args = [re.sub(r"\$\((\w+)-[^)]*\)|\$\(B\)/resources/(\w+)", lambda m: f"$({m[1] or m[2]})", arg.replace("$(BUILD_ROOT)", "$(B)")) for command in node["cmds"] for arg in command["cmd_args"]]
        self.assertIn("$(JDK21)/bin/java", args)
        self.assertIn(f"$(B)/{PROJECT}//nested/answer.yaml", args)

    def test_generator_archive_precedes_rooted_outputs(self):
        # The outputs follow the command: the OUT_DIR archive, then
        # OUT_NOAUTO; an argument written from a root variable stays text and
        # does not move its output ahead. The archive is the main output.
        self.generator()
        call = ("Generator -episode ${BINDIR}/generated/answer.episode.xml -d ${BINDIR}/generated/answer "
                f"CLASSPATH {PROJECT}/tool OUT_NOAUTO ${{BINDIR}}/generated/answer.episode.xml OUT_DIR ${{BINDIR}}/generated/answer")
        self.write("ya.make", f"JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nRUN_JAVA_PROGRAM({call})\nJAVA_SRCS(SRCDIR ${{BINDIR}}/generated/answer **/*)\nEND()\n")
        node = self.node(self.graph(), "generated/answer.episode.xml")
        self.assertEqual(len(node["outputs"]), 2)
        self.assertRegex(node["outputs"][0], rf"^\$\(B\)/{PROJECT}/generated/answer\.[0-9a-f]{{32}}\.gentar$")
        self.assertEqual(node["outputs"][1], f"$(B)/{PROJECT}/generated/answer.episode.xml")

    def test_generator_argument_outputs_lead(self):
        # Arguments naming an OUT or OUT_NOAUTO file become outputs in place
        # inside the arguments, ahead of the archive; the rest follow in
        # command order: OUT_NOAUTO, then OUT.
        self.generator()
        call = ("Generator Second.java First.java -d ${BINDIR}/generated/answer "
                f"CLASSPATH {PROJECT}/tool OUT Third.java First.java Second.java OUT_NOAUTO Late.txt OUT_DIR ${{BINDIR}}/generated/answer")
        self.write("ya.make", f"JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nRUN_JAVA_PROGRAM({call})\nJAVA_SRCS(SRCDIR ${{BINDIR}}/generated/answer **/*)\nEND()\n")
        node = self.node(self.graph(), "First.java")
        self.assertEqual(len(node["outputs"]), 5)
        self.assertEqual(node["outputs"][:2], [f"$(B)/{PROJECT}/Second.java", f"$(B)/{PROJECT}/First.java"])
        self.assertRegex(node["outputs"][2], rf"^\$\(B\)/{PROJECT}/generated/answer\.[0-9a-f]{{32}}\.gentar$")
        self.assertEqual(node["outputs"][3:], [f"$(B)/{PROJECT}/Late.txt", f"$(B)/{PROJECT}/Third.java"])

    def test_build_root_cwd(self):
        self.generator()
        self.write("ya.make", f"JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nRUN_JAVA_PROGRAM(Generator CLASSPATH {PROJECT}/tool CWD ${{ARCADIA_BUILD_ROOT}} OUT Generated.java)\nJAVA_SRCS(Generated.java)\nEND()\n")
        node = self.node(self.graph(), "Generated.java")
        command = next(command for command in node["cmds"] if "Generator" in command["cmd_args"])
        self.assertEqual(command["cwd"].replace("$(BUILD_ROOT)", "$(B)"), "$(B)")
