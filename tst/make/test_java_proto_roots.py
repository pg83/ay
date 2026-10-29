from tst.make.lib import MakeTest, PROJECT


class JavaProtoRootsTest(MakeTest):
    def java_consumer(self):
        self.write("ya.make", f"JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nJAVA_SRCS(Answer.java)\nPEERDIR({PROJECT}/schema)\nEND()\n")
        self.write("Answer.java", "public class Answer {}\n")
        self.write("schema/answer.proto", 'syntax = "proto3";\nmessage Answer {}\n')

    def include_args(self, node):
        return [arg.replace("$(SOURCE_ROOT)", "$(S)").replace("$(BUILD_ROOT)", "$(B)") for command in node["cmds"] for arg in command["cmd_args"] if arg.startswith("-I=")]

    def test_root_proto_namespace_uses_canonical_include(self):
        self.java_consumer()
        self.write("schema/ya.make", "PROTO_LIBRARY()\nPROTO_NAMESPACE(GLOBAL .)\nSET(PROTO_NAMESPACE \"\")\nSRCS(answer.proto)\nEND()\n")
        includes = self.include_args(self.node(self.graph(), "schema/answer.jsrc"))
        self.assertEqual(includes, ["-I=./", "-I=$(B)", "-I=$(S)", "-I=$(S)/contrib/libs/protobuf/src", "-I=$(S)"])

    def test_inherited_root_proto_include_is_deduplicated(self):
        self.java_consumer()
        self.write("schema/ya.make", f"PROTO_LIBRARY()\nSRCS(answer.proto)\nPEERDIR({PROJECT}/base)\nEND()\n")
        self.write("base/ya.make", "PROTO_LIBRARY()\nPROTO_NAMESPACE(GLOBAL .)\nSRCS(base.proto)\nEND()\n")
        self.write("base/base.proto", 'syntax = "proto3";\nmessage Base {}\n')
        includes = self.include_args(self.node(self.graph(), "schema/answer.jsrc"))
        self.assertEqual(includes, ["-I=./", "-I=$(B)", "-I=$(S)", "-I=$(S)/contrib/libs/protobuf/src", "-I=$(S)"])

    def test_namespace_exports_include_without_global_keyword(self):
        self.java_consumer()
        self.write("schema/ya.make", f"PROTO_LIBRARY()\nSRCS(answer.proto)\nPEERDIR({PROJECT}/base)\nEND()\n")
        self.write("schema/answer.proto", 'syntax = "proto3";\nimport "base.proto";\nmessage Answer { Base value = 1; }\n')
        self.write("base/ya.make", f"PROTO_LIBRARY()\nPROTO_NAMESPACE({PROJECT}/base)\nSRCS(base.proto)\nEND()\n")
        self.write("base/base.proto", 'syntax = "proto3";\nmessage Base {}\n')
        node = self.node(self.graph(), "schema/answer.jsrc")
        self.assertIn(f"-I=$(S)/{PROJECT}/base", self.include_args(node))
        self.assertIn(f"$(S)/{PROJECT}/base/base.proto", node["inputs"])

    def test_proto_generated_by_the_cpp_variant(self):
        self.write("ya.make", f"JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nJAVA_SRCS(Answer.java)\nPEERDIR({PROJECT}/schema)\nEND()\n")
        self.write("Answer.java", "public class Answer {}\n")
        self.write("schema/ya.make", (
            "PROTO_LIBRARY()\nSRCS(gen.proto)\nIF (GEN_PROTO)\n"
            f"    RUN_PROGRAM({PROJECT}/generator OUT_NOAUTO gen.proto CWD ${{ARCADIA_BUILD_ROOT}}/{PROJECT}/schema)\n"
            "ENDIF()\nEND()\n"
        ))
        self.write("generator/ya.make", "PROGRAM(generator)\nSRCS(main.cpp)\nEND()\n")
        self.write("generator/main.cpp", "int main() { return 0; }\n")
        graph = self.graph()
        java = self.node(graph, "schema/gen.jsrc")
        generated = self.node(graph, "schema/gen.proto")
        self.assertIn(f"$(B)/{PROJECT}/schema/gen.proto", java["inputs"])
        self.assertNotIn(f"$(S)/{PROJECT}/schema/gen.proto", java["inputs"])
        self.assertIn(generated["uid"], java["deps"])

    def test_cpp_variant_configured_from_java_keeps_object_order(self):
        self.write("ya.make", "RECURSE(\n    java\n    cpp\n)\n")
        self.write("java/ya.make", f"JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nJAVA_SRCS(Answer.java)\nPEERDIR({PROJECT}/schema)\nEND()\n")
        self.write("java/Answer.java", "public class Answer {}\n")
        self.write("cpp/ya.make", f"PROGRAM(answer)\nSRCS(main.cpp)\nPEERDIR({PROJECT}/schema)\nEND()\n")
        self.write("cpp/main.cpp", "int main() { return 0; }\n")
        self.write("schema/ya.make", (
            "PROTO_LIBRARY()\nSRCS(answer.proto)\n"
            f"GENERATE_ENUM_SERIALIZATION(${{ARCADIA_BUILD_ROOT}}/{PROJECT}/schema/answer.pb.h)\nEND()\n"
        ))
        self.write("schema/answer.proto", 'syntax = "proto3";\nenum Answer { ANSWER_UNKNOWN = 0; }\n')
        graph = self.graph()
        archives = [node for node in graph["graph"] if any(out.startswith(f"$(B)/{PROJECT}/schema/") and out.endswith(".a") for out in node["outputs"])]
        self.assertEqual(len(archives), 1, [node["outputs"] for node in archives])
        args = [arg.replace("$(BUILD_ROOT)", "$(B)") for command in archives[0]["cmds"] for arg in command["cmd_args"]]
        objects = [arg for arg in args if arg.startswith(f"$(B)/{PROJECT}/schema/") and arg.endswith(".o")]
        self.assertEqual(objects, [
            f"$(B)/{PROJECT}/schema/answer.pb.cc.o",
            f"$(B)/{PROJECT}/schema/answer.pb.h_serialized.cpp.o",
        ])

    def test_escaped_include_root(self):
        self.write("ya.make", f"JAVA_LIBRARY(answer)\nINCLUDE($\\{{ARCADIA_ROOT}}/{PROJECT}/settings.inc)\nJAVA_SRCS(Answer.java)\nEND()\n")
        self.write("settings.inc", "JDK_VERSION(17)\nWITH_KOTLIN()\nKOTLINC_FLAGS(-Xcontext-receivers)\n")
        self.write("Answer.java", "public class Answer {}\n")
        self.node(self.graph(), "answer.jar")

    def test_kotlin_friend_path_expands_unbound_variable(self):
        self.write("ya.make", "JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nWITH_KOTLIN()\nKOTLINC_FLAGS(-Xfriend-paths=$PROJECT_DIR$/out/production/other)\nJAVA_SRCS(Answer.kt)\nEND()\n")
        self.write("Answer.kt", "class Answer\n")
        args = [arg for command in self.node(self.graph(), "answer.jar")["cmds"] for arg in command["cmd_args"]]
        self.assertIn("-Xfriend-paths=$/out/production/other", args)
        self.assertNotIn("-Xfriend-paths=$PROJECT_DIR$/out/production/other", args)

    def test_java_proto_uses_cpp_variant_generated_source(self):
        self.write("ya.make", "RECURSE(java native)\n")
        self.write("java/ya.make", f"JAVA_LIBRARY(answer)\nJDK_VERSION(17)\nJAVA_SRCS(Answer.java)\nPEERDIR({PROJECT}/schema)\nEND()\n")
        self.write("java/Answer.java", "public class Answer {}\n")
        self.write("native/ya.make", f"LIBRARY()\nPEERDIR({PROJECT}/schema)\nEND()\n")
        self.write("schema/ya.make", f"PROTO_LIBRARY()\nSRCS(answer.proto)\nIF(GEN_PROTO)\nRUN_PROGRAM({PROJECT}/generator IN input.txt OUT_NOAUTO answer.proto)\nENDIF()\nEND()\n")
        self.write("schema/input.txt", "42\n")
        self.write("generator/ya.make", "PY3_PROGRAM(generator)\nPY_SRCS(MAIN main.py)\nEND()\n")
        self.write("generator/main.py", 'from pathlib import Path\nPath("answer.proto").write_text(\'syntax = "proto3"; message Answer {}\\n\')\n')
        graph = self.graph()
        node = self.node(graph, "schema/answer.jsrc")
        producer = self.node(graph, "schema/answer.proto")
        self.assertIn(f"$(B)/{PROJECT}/schema/answer.proto", node["inputs"])
        self.assertNotIn(f"$(S)/{PROJECT}/schema/answer.proto", node["inputs"])
        self.assertIn(producer["uid"], node["deps"])
        self.assertEqual(node["cmds"][0]["cwd"].replace("$(BUILD_ROOT)", "$(B)"), "$(B)")
