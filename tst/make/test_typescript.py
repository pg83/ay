import json

from tst.make.lib import MakeTest, PROJECT


class TypescriptTest(MakeTest):
    def package(self, module, macros="", flags=(), prepare_only=False):
        self.write("ya.make", f"{module}()\n{macros}\nEND()\n")
        self.write("package.json", json.dumps({"name": "@e2e/answer", "version": "1.0.0", "engines": {"node": "22.13.0"}, "files": ["src", "dist"], "scripts": {"nots:build": "tsc"}}))
        self.write("pnpm-lock.yaml", "lockfileVersion: '9.0'\nsettings:\n  autoInstallPeers: true\n  excludeLinksFromLockfile: false\nimporters:\n  .: {}\n")
        self.write("tsconfig.json", json.dumps({"compilerOptions": {"rootDir": "src", "outDir": "dist", "target": "ES2022"}, "include": ["src/**/*.ts"]}))
        self.write(".eslintrc.js", "module.exports = {};\n")
        self.write("build.js", "console.log(42);\n")
        self.write("src/answer.ts", "export const answer = 42;\n")
        if prepare_only:
            return
        graph = self.graph(*flags)
        if not flags:
            self.assertIn(self.node(graph, "output.tar")["uid"], graph["result"])
        return graph

    def test_package(self):
        self.package("TS_PACKAGE")

    def test_package_source_exclusions(self):
        self.package("TS_PACKAGE", prepare_only=True)
        self.write("a.yaml", "service: e2e\n")
        self.write("src/cache.tsbuildinfo", "{}\n")
        self.write(".npmrc", "ignore-scripts=true\n")
        node = self.node(self.graph(), "output.tar")
        self.assertNotIn(f"$(S)/{PROJECT}/a.yaml", node["inputs"])
        self.assertIn(f"$(S)/{PROJECT}/src/cache.tsbuildinfo", node["inputs"])
        self.assertNotIn(f"$(S)/{PROJECT}/.npmrc", node["inputs"])

    def test_typescript_compiler(self):
        self.package("TS_TSC", "TS_BUILD_OUTPUTS(dist)")

    def test_custom_build(self):
        self.package("TS_LIBRARY", "TS_BUILD_SCRIPT(nots:build node build.js)\nTS_BUILD_OUTPUTS(dist)")

    def test_typecheck_and_lint_graph(self):
        graph = self.package("TS_TSC", "TS_BUILD_OUTPUTS(dist)", ("-t",))
        self.assertTrue(any("eslint" in json.dumps(node.get("cmds", [])) for node in graph["graph"]))

    def test_custom_checks(self):
        graph = self.package("TS_PACKAGE", "TS_LINT(check node --check src/answer.ts)", ("-t",))
        self.assertTrue(any("run_ts_check" in json.dumps(node.get("cmds", [])) for node in graph["graph"]))

    def test_test_for(self):
        self.package("TS_PACKAGE")
        self.write("ya.make", "TS_PACKAGE()\nEND()\nRECURSE_FOR_TESTS(tests)\n")
        self.write("tests/ya.make", f"TS_TEST_FOR({PROJECT})\nTS_TEST(test node --test)\nEND()\n")
        graph = self.graph("-t")
        self.assertTrue(any("run_ts_check" in json.dumps(node.get("cmds", [])) for node in graph["graph"]))

    def test_build_variants(self):
        self.write("vite.config.ts", "export default {};\n")
        self.write("next.config.js", "module.exports = {};\n")
        for module, macros in (
            ("TS_VITE", "TS_VITE_CONFIG(vite.config.ts)\nTS_VITE_OUTPUT(dist)"),
            ("TS_NEXT", "TS_NEXT_CONFIG(next.config.js)\nTS_NEXT_BUILD_OPTIONS(--no-lint)\nTS_NEXT_OUTPUT(.next)"),
            ("TS_TSC", "TS_BUILD_OUTPUTS(dist)\nRUN_JAVASCRIPT_AFTER_BUILD(build.js IN src/answer.ts OUTDIR extra)\nVCS_INFO_FILE(FILE version.json)\nTS_OUTPUT_PREFIX(generated)\nTS_EXCLUDE_FILES_GLOB(**/ignored.ts)"),
        ):
            with self.subTest(module=module):
                self.package(module, macros)

    def test_node_modules_bundle(self):
        self.package("TS_PACKAGE", "WITH_NODE_MODULES(PROD)", prepare_only=True)
        package = json.loads((self.project / "package.json").read_text())
        package["dependencies"] = {"typescript": "5.9.3"}
        self.write("package.json", json.dumps(package))
        self.write("pnpm-lock.yaml", """lockfileVersion: '9.0'
settings:
  autoInstallPeers: true
  excludeLinksFromLockfile: false
importers:
  .:
    dependencies:
      typescript:
        specifier: 5.9.3
        version: 5.9.3
packages:
  typescript@5.9.3:
    resolution: {integrity: sha512-jl1vZzPDinLr9eUt3J/t7V6FgNEw9QjvBPdysz9KfQDD41fQrC2Y4vKQdiaUpFT4bXlb1RHhLpp8wtm6M5TgSw==, tarball: https://npm.yandex-team.ru/typescript/-/typescript-5.9.3.tgz}
snapshots:
  typescript@5.9.3: {}
""")
        for flags in ((), ("-DAUTOCHECK=yes",), ("-DTS_LOCAL_CLI=yes",)):
            with self.subTest(flags=flags):
                self.node(self.graph(*flags), "output.tar")

    def test_workspace_dependency(self):
        self.package("TS_PACKAGE", prepare_only=True)
        package = json.loads((self.project / "package.json").read_text())
        package["dependencies"] = {"@e2e/lib": "workspace:./lib"}
        self.write("package.json", json.dumps(package))
        self.write("pnpm-lock.yaml", "lockfileVersion: '9.0'\nimporters:\n  .:\n    dependencies:\n      '@e2e/lib':\n        specifier: workspace:./lib\n        version: link:lib\n")
        self.write("lib/ya.make", "TS_PACKAGE()\nEND()\n")
        self.write("lib/package.json", json.dumps({"name": "@e2e/lib", "version": "1.0.0", "engines": {"node": "22.13.0"}}))
        self.write("lib/pnpm-lock.yaml", "lockfileVersion: '9.0'\nimporters:\n  .: {}\n")
        graph = self.graph()
        self.node(graph, "lib/output.tar")

    def test_typescript_proto(self):
        self.write("ya.make", "PROTO_LIBRARY()\nONLY_TAGS(TS_PROTO TS_PREPARE_DEPS)\nTS_PROTO_PACKAGE_NAME(@e2e/answer)\nTS_PROTO_OPT(outputJsonMethods=false)\nSRCS(answer.proto)\nEND()\n")
        self.write("answer.proto", 'syntax = "proto3"; package e2e; message Answer { int32 value = 1; }\n')
        graph = self.graph()
        self.assertEqual(graph["result"], [self.node(graph, "output.tar")["uid"]])
