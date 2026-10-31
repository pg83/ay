from tst.make.lib import MakeTest, PROJECT


class LateProtoCommandScopeTest(MakeTest):
    def test_generated_source_can_include_proto_headers_from_a_shared_library(self):
        for explicit_source in (False, True):
            with self.subTest(explicit_source=explicit_source):
                self.write("ya.make", "RECURSE(schema consumer)\n")
                self.write("schema/ya.make", "DLL(schema)\nSRCS(answer.proto)\nEND()\n")
                self.write("schema/answer.proto", 'syntax = "proto2";\nmessage Answer { optional int32 value = 1; }\n')
                sources = "SRCS(generated.cpp)\n" if explicit_source else ""
                output = "OUT_NOAUTO" if explicit_source else "OUT"
                self.write("consumer/ya.make", f"LIBRARY(answer)\nSET(DONT_RESOLVE_INCLUDES yes)\nFROM_SANDBOX(FILE 123456789 {output} generated.cpp OUTPUT_INCLUDES ${{ARCADIA_BUILD_ROOT}}/{PROJECT}/schema/answer.pb.h)\n{sources}END()\n")
                graph = self.graph()
                self.node(graph, "consumer/generated.cpp.o")
