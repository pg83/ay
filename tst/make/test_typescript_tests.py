import json

from tst.make.lib import MakeTest, PROJECT


class TypescriptTests(MakeTest):
    def package(self, macros=""):
        self.write("ya.make", f"TS_PACKAGE()\n{macros}\nEND()\nRECURSE_FOR_TESTS(tests)\n")
        self.write("package.json", json.dumps({"name": "@e2e/answer", "version": "1.0.0", "engines": {"node": "22.13.0"}}))
        self.write("pnpm-lock.yaml", "lockfileVersion: '9.0'\nimporters:\n  .: {}\n")
        self.write("tsconfig.json", json.dumps({"compilerOptions": {"target": "ES2022"}, "include": ["src/**/*.ts"]}))
        self.write("src/answer.ts", "export const answer = 42;\n")

    def test_jest(self):
        self.package()
        self.write("jest.config.js", "module.exports = {};\n")
        self.write("tests/ya.make", f"TS_TEST_JEST_FOR({PROJECT})\nTS_TEST_SRCS(../src)\nTS_TEST_CONFIG(../jest.config.js)\nTS_TEST_DATA(data/*.txt)\nEND()\n")
        self.write("src/answer.test.ts", 'import {answer} from "./answer";\ntest("answer", () => expect(answer).toBe(42));\n')
        self.write("src/__mocks__/mock.ts", "export default 17;\n")
        self.write("tests/data/value.txt", "42\n")
        graph = self.graph("-t")
        self.assertTrue(any("run_jest" in json.dumps(node.get("cmds", [])) for node in graph["graph"]))

    def test_vitest_imports(self):
        self.package()
        self.write("vitest.config.ts", "export default {};\n")
        self.write("tests/ya.make", f"TS_TEST_VITEST_FOR({PROJECT})\nTS_TEST_SRCS(../src)\nTS_TEST_CONFIG(../vitest.config.ts)\nTS_CONFIG(../tsconfig.json)\nEND()\n")
        self.write("src/answer.spec.ts", 'import {answer} from "./answer.js";\nexport {value} from "./nested";\nconst other = require("./other.ts");\n')
        self.write("src/nested/index.ts", "export const value = 17;\n")
        self.write("src/other.ts", "export const other = 12;\n")
        graph = self.graph("-t")
        self.assertTrue(any("run_vitest" in json.dumps(node.get("cmds", [])) for node in graph["graph"]))

    def test_style_configs(self):
        self.package("TS_TYPECHECK(tsconfig.json)\nTS_ESLINT_CONFIG(eslint.config.js)\nTS_STYLELINT(stylelint.config.js)\nTS_BIOME(biome.json)")
        self.write("eslint.config.js", "module.exports = {};\n")
        self.write("stylelint.config.js", "module.exports = {};\n")
        self.write("biome.json", "{}\n")
        self.write("src/style.css", "body { color: blue; }\n")
        self.write("tests/ya.make", f"TS_TEST_FOR({PROJECT})\nTS_TEST(test node --test)\nEND()\n")
        graph = self.graph("-t")
        self.assertTrue(any("run_ts_check" in json.dumps(node.get("cmds", [])) for node in graph["graph"]))

    def test_import_scan_string_ends(self):
        # The import scanner closes a string at a quote that follows a
        # non-backslash character of its body: an empty string or one ending
        # in an escaped backslash runs on to the next quote, hiding imports.
        self.package()
        self.write("vitest.config.ts", "export default {};\n")
        self.write("tests/ya.make", f"TS_TEST_VITEST_FOR({PROJECT})\nTS_TEST_SRCS(../src)\nTS_TEST_CONFIG(../vitest.config.ts)\nEND()\n")
        self.write("src/first.ts", "export const first = 1;\n")
        self.write("src/later.ts", "export const later = 2;\n")
        for literal in ("''", "'\\\\'", '""'):
            with self.subTest(literal=literal):
                self.write("src/answer.spec.ts", f'const first = await import("./first");\nlet value = {literal};\nconst later = await import("./later");\n')
                graph = self.graph("-t")
                nodes = [node for node in graph["graph"] if any(path.endswith(".ts_test.fake") for path in node["outputs"])]
                self.assertEqual(len(nodes), 1)
                self.assertIn(f"$(S)/{PROJECT}/src/first.ts", nodes[0]["inputs"])
                self.assertNotIn(f"$(S)/{PROJECT}/src/later.ts", nodes[0]["inputs"])
