import json

from tst.make.lib import MakeTest, PROJECT


class SandboxTypescriptReuseTest(MakeTest):
    def test_keep_going_keeps_healthy_roots_after_nested_typescript_sandbox_collisions(self):
        self.write("ya.make", f"LIBRARY(answer)\nBASE_CODEGEN({PROJECT}/package first)\nBASE_CODEGEN({PROJECT}/proto second)\nSRCS(first.cpp second.cpp)\nEND()\nRECURSE(healthy)\n")
        for stem in ("first", "second"):
            self.write(stem + ".in", "42\n")
        self.write("package/ya.make", f"TS_PACKAGE()\nPEERDIR({PROJECT}/proto)\nEND()\n")
        self.write("leaf/ya.make", "TS_PACKAGE()\nEND()\n")
        for directory in ("package", "leaf"):
            self.write(directory + "/package.json", json.dumps({"name": "@e2e/" + directory, "version": "1.0.0", "engines": {"node": "22.13.0"}, "files": ["value.txt"]}))
            self.write(directory + "/pnpm-lock.yaml", "lockfileVersion: '9.0'\nsettings:\n  autoInstallPeers: true\n  excludeLinksFromLockfile: false\nimporters:\n  .: {}\n")
            self.write(directory + "/value.txt", "42\n")
        self.write("proto/ya.make", f"PROTO_LIBRARY(schema)\nONLY_TAGS(CPP_PROTO TS_PROTO TS_PREPARE_DEPS)\nTS_PROTO_PACKAGE_NAME(@e2e/schema)\nIF(TS_PROTO OR TS_PROTO_PREPARE_DEPS)\nTS_PROTO_NON_RECURSIVE()\nSET(PEERDIR_TAGS CPP_PROTO)\nPEERDIR({PROJECT}/proto {PROJECT}/leaf)\nENDIF()\nFROM_SANDBOX(FILE 123456789 OUT_NOAUTO package.json)\nSRCS(answer.proto)\nEND()\n")
        self.write("proto/answer.proto", 'syntax = "proto3"; message Answer { int32 value = 1; }\n')
        self.write("healthy/ya.make", "PROGRAM(answer)\nSRCS(main.cpp)\nEND()\n")
        self.write("healthy/main.cpp", "int main() { return 0; }\n")
        graph = self.graph("-k")
        self.assertIn(self.node(graph, "healthy/answer")["uid"], graph["result"])
