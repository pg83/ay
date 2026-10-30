import base64
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

    def test_test_data_globs_keep_exact_files_and_matching_subdirectories(self):
        self.package()
        self.write("jest.config.js", "module.exports = {};\n")
        self.write("tests/ya.make", f"TS_TEST_JEST_FOR({PROJECT})\nTS_TEST_SRCS(../src)\nTS_TEST_CONFIG(../jest.config.js)\nTS_TEST_DATA(data/exact.txt data/group*/nested.txt data/trailing*)\nEND()\n")
        self.write("src/answer.test.ts", 'test("answer", () => expect(6 * 7).toBe(42));\n')
        for name in ("exact.txt", "group1/nested.txt", "group2/nested.txt", "trailing"):
            self.write("tests/data/" + name, "42\n")
        suite = self.node(self.graph("-t"), "tests/test-results/jest/meta.json")
        args = [arg for command in suite["cmds"] for arg in command["cmd_args"]]
        start = args.index("--test-data-dirs") + 1
        dirs = {arg.removeprefix("$(SOURCE_ROOT)/").removeprefix("$(S)/")
                for arg in args[start:args.index("--config", start)]}
        self.assertEqual(dirs, {f"{PROJECT}/tests/data" + suffix for suffix in ("", "/group1", "/group2")})

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

    def test_libatomic_check_env(self):
        # Node.js 25+ on Linux links libatomic from a resource: every TS check
        # runs with only LD_LIBRARY_PATH in its environment.
        self.package("TS_LINT(lint)")
        self.write("package.json", json.dumps({"name": "@e2e/answer", "version": "1.0.0", "engines": {"node": "26.10.0"}, "scripts": {"lint": "true", "test": "true"}}))
        self.write("tests/ya.make", f"TS_TEST_FOR({PROJECT})\nTS_TEST(test)\nENV(E2E_ANSWER=42)\nEND()\n")
        graph = self.graph("-t", "--target-platform=default-linux-x86_64", "--host-platform=default-linux-x86_64")
        for output in ("test-results/ts_lint_lint/meta.json", "tests/test-results/ts_test_test/meta.json"):
            with self.subTest(output=output):
                args = [arg for command in self.node(graph, output)["cmds"] for arg in command["cmd_args"]]
                env = [args[i + 1] for i, arg in enumerate(args) if arg == "--env"]
                self.assertEqual(len(env), 1)
                self.assertRegex(env[0], r"^LD_LIBRARY_PATH=\$\((B\)/resources/)?LIBATOMIC_1_2_0\b")

    def test_check_recipes_follow_module_recipes(self):
        # TS checks set up their builder recipes in the module epilogue, after
        # the USE_RECIPE calls of ya.make; jest sets them up before.
        self.package("TS_LINT(lint)")
        self.write("package.json", json.dumps({"name": "@e2e/answer", "version": "1.0.0", "engines": {"node": "22.13.0"}, "scripts": {"lint": "true", "test": "true"}}))
        self.write("jest.config.js", "module.exports = {};\n")
        self.write("src/answer.test.ts", 'import {answer} from "./answer";\ntest("answer", () => expect(answer).toBe(42));\n')
        self.write("recipe/ya.make", "PY3_PROGRAM(recipe)\nPY_SRCS(MAIN main.py)\nEND()\n")
        self.write("recipe/main.py", "pass\n")
        for module, body, output in (("TS_TEST_FOR", "TS_TEST(test)", "tests/test-results/ts_test_test/meta.json"), ("TS_TEST_JEST_FOR", "TS_TEST_SRCS(../src)\nTS_TEST_CONFIG(../jest.config.js)", "tests/test-results/jest/meta.json")):
            with self.subTest(module=module):
                self.write("tests/ya.make", f"{module}({PROJECT})\nDEPENDS({PROJECT}/recipe)\nUSE_RECIPE({PROJECT}/recipe/recipe --answer 42)\n{body}\nEND()\n")
                args = [arg for command in self.node(self.graph("-t"), output)["cmds"] for arg in command["cmd_args"]]
                recipes = [line.strip() for line in base64.b64decode(args[args.index("--recipes") + 1]).decode().splitlines() if line.strip()]
                self.assertEqual(recipes.index(f"{PROJECT}/recipe/recipe --answer 42"), 0 if module == "TS_TEST_FOR" else len(recipes) - 1)
        with self.subTest(module="TS_TSC"):
            self.write("ya.make", f"TS_TSC()\nTS_BUILD_OUTPUTS(dist)\nTS_TYPECHECK(tsconfig.json)\nDEPENDS({PROJECT}/recipe)\nUSE_RECIPE({PROJECT}/recipe/recipe --answer 42)\nEND()\n")
            self.write("tests/ya.make", f"TS_TEST_FOR({PROJECT})\nTS_TEST(test)\nEND()\n")
            self.write(".eslintrc.js", "module.exports = {};\n")
            args = [arg for command in self.node(self.graph("-t"), "test-results/tsc_typecheck/meta.json")["cmds"] for arg in command["cmd_args"]]
            recipes = [line.strip() for line in base64.b64decode(args[args.index("--recipes") + 1]).decode().splitlines() if line.strip()]
            self.assertEqual(recipes.index(f"{PROJECT}/recipe/recipe --answer 42"), 0)

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
