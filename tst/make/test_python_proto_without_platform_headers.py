from tst.make.lib import MakeTest


class PythonProtoWithoutPlatformHeadersTest(MakeTest):
    def test_python_proto_variants_work_without_implicit_platform_headers(self):
        self.write("ya.make", "PROTO_LIBRARY()\nINCLUDE_TAGS(PY3_PROTO)\nSET(NEED_PLATFORM_PEERDIRS no)\nSRCS(answer.proto)\nEND()\n")
        self.write("answer.proto", 'syntax = "proto3"; message Answer { int32 value = 1; }\n')
        graph = self.graph()
        self.node(graph, "answer.pb.cc")
        self.node(graph, "answer__intpy3___pb2.py")
