import json

from tst.make.lib import MakeTest, PROJECT


class ProtoTypescriptInputsTest(MakeTest):
    def test_protocol_generators_keep_typescript_package_preparation_dependencies(self):
        self.write("ya.make", "RECURSE(bundle schema)\n")
        self.write("bundle/ya.make", "TS_PACKAGE()\nEND()\n")
        self.write("bundle/package.json", json.dumps({"name": "@e2e/schema", "version": "1.0.0", "engines": {"node": "22.13.0"}, "files": ["schema.proto"]}))
        self.write("bundle/pnpm-lock.yaml", "lockfileVersion: '9.0'\nsettings:\n  autoInstallPeers: true\n  excludeLinksFromLockfile: false\nimporters:\n  .: {}\n")
        self.write("bundle/schema.proto", 'syntax = "proto3"; message Answer { int32 value = 1; }\n')
        self.write("schema/ya.make", f"PROTO_LIBRARY(schema)\nONLY_TAGS(CPP_PROTO)\nADDINCL(contrib/libs/protobuf/src)\nCOPY_FILE(stub.h ${{ARCADIA_BUILD_ROOT}}/{PROJECT}/bundle/output.pb.h)\nRUN_PROGRAM({PROJECT}/tool IN ${{ARCADIA_BUILD_ROOT}}/{PROJECT}/bundle/output.tar STDOUT generated.proto OUTPUT_INCLUDES {PROJECT}/bundle/output.tar)\nEND()\n")
        self.write("schema/stub.h", "#pragma once\n")
        self.write("tool/ya.make", "PROGRAM(generator)\nSRCS(main.cpp)\nEND()\n")
        self.write("tool/main.cpp", '#include <cstdio>\nint main() { std::puts("syntax = \\"proto3\\"; message Answer { int32 value = 1; }"); }\n')
        graph = self.graph()
        generated = self.node(graph, "schema/generated.proto")
        bundle = self.node(graph, "bundle/output.tar")
        self.assertIn(self.node(graph, "bundle/package.json")["uid"], bundle["deps"])
        self.assertIn(bundle["uid"], generated["deps"])
        self.assertIn(generated["uid"], self.node(graph, "schema/generated.pb.h")["deps"])
