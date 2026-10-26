from tst.make.lib import MakeTest, PROJECT


class ProtoSourceContextsTest(MakeTest):
    def test_go_proto_consumes_generated_source(self):
        self.write("ya.make", "RECURSE(consumer)\n")
        self.write("consumer/ya.make", f"GO_LIBRARY()\nSRCS(answer.go)\nPEERDIR({PROJECT}/schema)\nEND()\n")
        self.write("consumer/answer.go", f'package consumer\nimport _ "a.yandex-team.ru/{PROJECT}/schema"\n')
        self.write("schema/ya.make", f"PROTO_LIBRARY()\nIF(GEN_PROTO)\nRUN_PROGRAM({PROJECT}/generator OUT_NOAUTO ${{BINDIR}}/answer.proto OUTPUT_INCLUDES {PROJECT}/schema/dependency.proto)\nENDIF()\nSRCS(answer.proto)\nEND()\n")
        self.write("schema/dependency.proto", 'syntax = "proto3"; package e2e; message Dependency {}\n')
        self.write("generator/ya.make", "PY3_PROGRAM(generator)\nPY_SRCS(main.py)\nEND()\n")
        self.write("generator/main.py", "print('generated')\n")
        graph = self.graph()
        producer = self.node(graph, "schema/answer.proto")
        generated = self.node(graph, "schema/answer.pb.go")
        self.assertIn(f"$(B)/{PROJECT}/schema/answer.proto", generated["inputs"])
        self.assertIn(f"$(S)/{PROJECT}/schema/dependency.proto", generated["inputs"])
        self.assertIn(producer["uid"], generated["deps"])

    def test_descriptor_keeps_local_proto_addincl_in_its_module(self):
        self.write("ya.make", f"PROTO_LIBRARY()\nONLY_TAGS(DESC_PROTO)\nPEERDIR({PROJECT}/schema)\nSRCS(answer.proto)\nEND()\nRECURSE(schema)\n")
        self.write("answer.proto", 'syntax = "proto3"; package answer; message Answer {}\n')
        self.write("schema/ya.make", f"PROTO_LIBRARY()\nONLY_TAGS(DESC_PROTO)\nPROTO_ADDINCL({PROJECT}/include)\nSRCS(dependency.proto)\nEND()\n")
        self.write("schema/dependency.proto", 'syntax = "proto3"; package dependency; message Dependency {}\n')
        self.write("include/extra.proto", 'syntax = "proto3"; package extra; message Extra {}\n')
        graph = self.graph()
        include = f"-I=$(S)/{PROJECT}/include"
        def args(output):
            return [arg.replace("$(SOURCE_ROOT)", "$(S)") for command in self.node(graph, output)["cmds"] for arg in command["cmd_args"]]
        self.assertIn(include, args("schema/dependency.proto.desc"))
        self.assertNotIn(include, args("answer.proto.desc"))

    def test_proto_addincl_is_passed_to_generators(self):
        self.write("ya.make", f"PROTO_LIBRARY()\nONLY_TAGS(CPP_PROTO)\nPROTO_ADDINCL({PROJECT}/include)\nSRCS(answer.proto)\nEND()\n")
        self.write("answer.proto", 'syntax = "proto3"; package e2e; message Answer {}\n')
        self.write("include/dependency.proto", 'syntax = "proto3"; package e2e; message Dependency {}\n')
        graph = self.graph()
        node = self.node(graph, "answer.pb.cc")
        args = [arg.replace("$(SOURCE_ROOT)", "$(S)") for command in node["cmds"] for arg in command["cmd_args"]]
        self.assertIn(f"-I=$(S)/{PROJECT}/include", args)

    def test_java_proto_source_directory_controls_generated_path(self):
        self.write("ya.make", "RECURSE(schema)\n")
        self.write("schema/ya.make", f"PROTO_LIBRARY()\nONLY_TAGS(JAVA_PROTO)\nSRCDIR(${{ARCADIA_ROOT}}/{PROJECT}/src)\nSRCS(answer.proto)\nEND()\n")
        self.write("src/answer.proto", 'syntax = "proto3"; package e2e; message Answer {}\n')
        graph = self.graph()
        producer = self.node(graph, "src/answer.jsrc")
        jars = [node for node in graph["graph"] if any(path.startswith(f"$(B)/{PROJECT}/schema/") and path.endswith(".jar") for path in node["outputs"])]
        self.assertTrue(jars)
        self.assertTrue(any(f"$(B)/{PROJECT}/src/answer.jsrc" in node["inputs"] and producer["uid"] in node["deps"] for node in jars))

    def test_java_common_proto_version_override(self):
        self.write("ya.make", "PROTO_LIBRARY()\nONLY_TAGS(JAVA_PROTO)\nSET(JAVA_PROTO_COMMON_VERSION 2.13.0)\nGRPC()\nUSE_COMMON_GOOGLE_APIS()\nSRCS(answer.proto)\nEND()\n")
        self.write("answer.proto", 'syntax = "proto3"; package e2e; message Answer {}\n')
        graph = self.graph()
        jars = [node for node in graph["graph"] if any(path.startswith(f"$(B)/{PROJECT}/") and path.endswith(".jar") for path in node["outputs"])]
        self.assertTrue(jars)
        expected = "$(B)/contrib/java/com/google/api/grpc/proto-google-common-protos/2.13.0/proto-google-common-protos-2.13.0.jar"
        self.assertTrue(any(expected in node["inputs"] for node in jars))

    def test_java_and_descriptor_local_proto_addincl(self):
        for tag, output in (("JAVA_PROTO", "answer.jsrc"), ("DESC_PROTO", "answer.proto.desc")):
            with self.subTest(tag=tag):
                self.write("ya.make", f"PROTO_LIBRARY()\nONLY_TAGS({tag})\nPROTO_ADDINCL({PROJECT}/include)\nSRCS(answer.proto)\nEND()\n")
                self.write("answer.proto", 'syntax = "proto3"; package e2e; message Answer {}\n')
                self.write("include/dependency.proto", 'syntax = "proto3"; package e2e; message Dependency {}\n')
                node = self.node(self.graph(), output)
                args = [arg.replace("$(SOURCE_ROOT)", "$(S)") for command in node["cmds"] for arg in command["cmd_args"]]
                self.assertIn(f"-I=$(S)/{PROJECT}/include", args)
