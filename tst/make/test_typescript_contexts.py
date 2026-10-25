import json

from tst.make.lib import MakeTest, PROJECT


class TypescriptContextsTest(MakeTest):
    def test_proto_opt_in(self):
        self.write("ya.make", "PROTO_LIBRARY()\nINCLUDE_TAGS(TS_PROTO TS_PREPARE_DEPS)\nONLY_TAGS(TS_PROTO TS_PREPARE_DEPS)\nTS_PROTO_PACKAGE_NAME(@e2e/answer)\nTS_PROTO_OPT(outputJsonMethods=false)\nGRPC()\nUSE_COMMON_GOOGLE_APIS()\nSRCS(answer.proto)\nEND()\n")
        self.write("answer.proto", 'syntax = "proto3";\npackage e2e;\nmessage Answer { int32 value = 1; }\n')
        for flags in ((), ("-DAUTOCHECK=yes",)):
            with self.subTest(flags=flags):
                self.node(self.graph(*flags), "output.tar")

    def test_proto_workspace(self):
        self.write("ya.make", f"PROTO_LIBRARY()\nINCLUDE_TAGS(TS_PROTO TS_PREPARE_DEPS)\nONLY_TAGS(TS_PROTO TS_PREPARE_DEPS)\nIF(TS_PROTO)\nTS_PROTO_NON_RECURSIVE()\nENDIF()\nPEERDIR({PROJECT}/peer)\nSRCS(answer.proto)\nEND()\n")
        self.write("package.json", json.dumps({"name": "@e2e/answer", "version": "1.0.0", "engines": {"node": "22.13.0"}, "dependencies": {"@e2e/peer": "workspace:./peer"}}))
        self.write("pnpm-lock.yaml", "lockfileVersion: '9.0'\nimporters:\n  .:\n    dependencies:\n      '@e2e/peer':\n        specifier: workspace:./peer\n        version: link:peer\n")
        self.write("tsconfig.json", '{"compilerOptions":{"outDir":"dist"}}\n')
        self.write("answer.proto", f'syntax = "proto3";\npackage e2e;\nimport "{PROJECT}/peer/base.proto";\nmessage Answer {{ Base base = 1; }}\n')
        self.write("peer/ya.make", "PROTO_LIBRARY()\nINCLUDE_TAGS(TS_PROTO TS_PREPARE_DEPS)\nTS_PROTO_PACKAGE_NAME(@e2e/peer)\nSRCS(base.proto)\nEND()\n")
        self.write("peer/base.proto", 'syntax = "proto3";\npackage e2e;\nmessage Base { int32 value = 1; }\n')
        self.node(self.graph(), "output.tar")

    def test_lockfile_v6(self):
        self.write("ya.make", "TS_PACKAGE()\nWITH_NODE_MODULES()\nDISABLE_HERMETIC_NODE_MODULES()\nEND()\n")
        self.write("package.json", json.dumps({"name": "@e2e/answer", "version": "1.0.0", "engines": {"node": "22.13.0"}, "dependencies": {"typescript": "5.9.3"}}))
        self.write("pnpm-lock.yaml", "lockfileVersion: '6.0'\ndependencies:\n  typescript:\n    specifier: 5.9.3\n    version: 5.9.3\npackages:\n  /typescript@5.9.3:\n    resolution:\n      integrity: sha512-jl1vZzPDinLr9eUt3J/t7V6FgNEw9QjvBPdysz9KfQDD41fQrC2Y4vKQdiaUpFT4bXlb1RHhLpp8wtm6M5TgSw==\n")
        self.node(self.graph(), "output.tar")

    def test_test_depends_on_build(self):
        self.write("ya.make", "TS_TSC()\nNO_LINT()\nTS_BUILD_OUTPUTS(dist)\nEND()\nRECURSE_FOR_TESTS(tests)\n")
        self.write("package.json", json.dumps({"name": "@e2e/answer", "version": "1.0.0", "engines": {"node": "22.13.0"}, "scripts": {"nots:build": "tsc"}}))
        self.write("pnpm-lock.yaml", "lockfileVersion: '9.0'\nimporters:\n  .: {}\n")
        self.write("tsconfig.json", '{"compilerOptions":{"rootDir":"src","outDir":"dist"},"include":["src/**/*.ts"]}\n')
        self.write("src/answer.ts", "export const answer = 42;\n")
        self.write("tests/ya.make", f"TS_TEST_FOR({PROJECT})\nTS_TEST(test node --test)\nTS_TEST_DEPENDS_ON_BUILD()\nEND()\n")
        self.node(self.graph("-t"), "output.tar")
