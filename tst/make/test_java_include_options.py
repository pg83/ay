from tst.make.lib import MakeTest, PROJECT


class JavaIncludeOptionsTest(MakeTest):
    def client(self):
        self.write("ya.make", f"JAVA_PROGRAM(answer)\nJDK_VERSION(17)\nJAVA_SRCS(Main.java)\nPEERDIR({PROJECT}/schema)\nEND()\n")
        self.write("Main.java", "public class Main {}\n")
        for directory in ("local", "global"):
            self.write(directory + "/.keep", "")

    def test_proto_include_scopes_and_appended_compiler_flags(self):
        self.client()
        self.write("schema/ya.make", f'''PROTO_LIBRARY()
SRCS(answer.proto)
ADDINCL(FOR proto {PROJECT}/local GLOBAL FOR proto {PROJECT}/global)
SET_APPEND(E2E_PROTO_FLAGS --fatal_warnings)
SET_APPEND(E2E_PROTO_FLAGS --experimental_allow_proto3_optional)
SET_APPEND(_PROTOC_FLAGS ${{E2E_PROTO_FLAGS}})
END()
''')
        self.write("schema/answer.proto", 'syntax = "proto3"; message Answer { optional int32 value = 1; }\n')
        node = self.node(self.graph(), "schema/answer.jsrc")
        args = [arg.replace("$(SOURCE_ROOT)", "$(S)") for command in node["cmds"] for arg in command["cmd_args"]]
        for directory in ("local", "global"):
            self.assertIn(f"-I=$(S)/{PROJECT}/{directory}", args)
        for flag in ("--fatal_warnings", "--experimental_allow_proto3_optional"):
            self.assertIn(flag, args)

    def test_flatc_include_scopes_and_compiler_flags(self):
        self.client()
        self.write("schema/ya.make", f"FBS_LIBRARY()\nSRCS(answer.fbs)\nADDINCL(FOR flatc {PROJECT}/local GLOBAL FOR flatc {PROJECT}/global)\nFLATC_FLAGS(--gen-object-api)\nEND()\n")
        self.write("schema/answer.fbs", "namespace e2e; table Answer { value:int; } root_type Answer;\n")
        node = self.node(self.graph(), "schema/answer.fbs.jsrc")
        args = [arg.replace("$(SOURCE_ROOT)", "$(S)") for command in node["cmds"] for arg in command["cmd_args"]]
        for directory in ("local", "global"):
            self.assertIn(f"$(S)/{PROJECT}/{directory}", args)
        self.assertIn("--gen-object-api", args)
