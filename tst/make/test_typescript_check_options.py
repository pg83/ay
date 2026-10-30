import base64
import json

from tst.make.lib import MakeTest, PROJECT


class TypescriptCheckOptionsTest(MakeTest):
    def test_test_for_ignores_legacy_after_build_javascript(self):
        self.package("TS_PACKAGE")
        self.write("ya.make", (self.project / "ya.make").read_text() + "RECURSE_FOR_TESTS(tests)\n")
        self.write("tests/ya.make", f"TS_TEST_FOR({PROJECT})\nTS_TEST(check:strict!)\nRUN_JAVASCRIPT_AFTER_BUILD(after.js)\nEND()\n")
        self.write("tests/after.js", "console.log(42);\n")
        self.node(self.graph("-t"), "tests/test-results/ts_test_check_strict_/meta.json")

    def test_configured_builder_inputs_are_tracked(self):
        self.package("TS_PACKAGE")
        flags = {"E2E_NOTS_INPUTS": f"{PROJECT}/builder_extra.py", "NOTS_TOOL": "${YMAKE_PYTHON3} ${input:E2E_NOTS_INPUTS}"}
        config = (self.root / "ya.conf").read_text() + "\n[flags]\n"
        self.write_tree("ya.conf", config + "\n".join(key + " = " + json.dumps(value) for key, value in flags.items()) + "\n")
        self.write("builder_extra.py", "pass\n")
        prepared = self.node(self.graph(), "package.json")
        self.assertIn(f"$(S)/{PROJECT}/builder_extra.py", prepared["inputs"])

    def test_new_modules_reject_after_build_javascript(self):
        self.write("build.js", "console.log(42);\n")
        for module in ("TS_PACKAGE", "TS_LIBRARY"):
            with self.subTest(module=module):
                self.package(module, "TS_BUILD_SCRIPT(nots:build)\nRUN_JAVASCRIPT_AFTER_BUILD(build.js)")
                self.assertNotEqual(self.make().returncode, 0)

    def test_after_build_javascript_requires_a_script(self):
        self.package(macros="RUN_JAVASCRIPT_AFTER_BUILD()")
        self.assertNotEqual(self.make().returncode, 0)

    def test_after_build_javascript_keeps_options_and_input_dependencies(self):
        self.package(macros="RUN_JAVASCRIPT_AFTER_BUILD(build.js --mode=strict IN input.txt OUTDIR extra)")
        package = json.loads((self.project / "package.json").read_text())
        package["files"].append("extra")
        self.write("package.json", json.dumps(package))
        self.write("build.js", "console.log(42);\n")
        self.write("input.txt", "42\n")
        built = self.node(self.graph(), "output.tar")
        args = [arg for command in built["cmds"] for arg in command["cmd_args"]]
        self.assertTrue(any("node build.js --mode=strict" in arg for arg in args), args)
        self.assertIn(f"$(S)/{PROJECT}/input.txt", built["inputs"])

    def test_build_environment_rejects_missing_values(self):
        for macro in ("TS_BUILD_ENV(E2E_MISSING)", "SET(TS_BUILD_ENV E2E_MISSING)"):
            with self.subTest(macro=macro):
                self.package(macros=macro)
                self.assertNotEqual(self.make().returncode, 0)

    def test_common_nots_configuration_is_a_prepare_input(self):
        self.package("TS_PACKAGE")
        package = json.loads((self.project / "package.json").read_text())
        package["nots"] = {"commonConfigPath": "common.json"}
        self.write("package.json", json.dumps(package))
        self.write("common.json", "{}\n")
        prepared = self.node(self.graph(), "package.json")
        self.assertIn(f"$(S)/{PROJECT}/common.json", prepared["inputs"])

    def test_prebuilder_path_is_passed_to_node_module_preparation(self):
        self.package("TS_PACKAGE", "WITH_NODE_MODULES()")
        package = json.loads((self.project / "package.json").read_text())
        package["usePrebuilder"] = True
        self.write("package.json", json.dumps(package))
        commands = [arg for node in self.graph("-DAUTOCHECK=yes")["graph"] for command in node["cmds"] for arg in command["cmd_args"]]
        self.assertIn("--yatool-prebuilder-path", commands)
        self.assertTrue(any(arg.endswith("/node_modules/@yatool/prebuilder") for arg in commands))

    def test_legacy_typechecking_can_be_disabled(self):
        self.package(macros="TS_TYPECHECK()\nNO_TS_TYPECHECK()")
        graph = self.graph("-t")
        self.node(graph, "test-results/eslint/meta.json")
        self.assertFalse(any(path.endswith("/tsc_typecheck/meta.json") for node in graph["graph"] for path in node["outputs"]))

    def test_script_checks_require_a_script_name(self):
        for macro in ("TS_TEST()", "TS_LINT()"):
            with self.subTest(macro=macro):
                self.package("TS_PACKAGE", macro)
                self.assertNotEqual(self.make("-t").returncode, 0)

    def test_empty_output_prefix_uses_the_module_directory(self):
        self.package("TS_PACKAGE", "TS_OUTPUT_PREFIX()")
        args = [arg for command in self.node(self.graph(), "output.tar")["cmds"] for arg in command["cmd_args"]]
        self.assertIn("--output-prefix=" + PROJECT, args)

    def test_trailing_double_star_excludes_nested_source_paths(self):
        self.package("TS_PACKAGE", "TS_EXCLUDE_FILES_GLOB(src/ignored**)")
        for name in ("ignored.ts", "ignored/nested.ts", "retained.ts"):
            self.write("src/" + name, "export const value = 42;\n")
        inputs = self.node(self.graph(), "output.tar")["inputs"]
        self.assertIn(f"$(S)/{PROJECT}/src/retained.ts", inputs)
        for name in ("ignored.ts", "ignored/nested.ts"):
            self.assertNotIn(f"$(S)/{PROJECT}/src/{name}", inputs)

    def test_generic_test_module_rejects_relative_target(self):
        self.package("TS_PACKAGE")
        self.write("ya.make", (self.project / "ya.make").read_text() + "RECURSE_FOR_TESTS(tests)\n")
        self.write("tests/ya.make", "TS_TEST_FOR(RELATIVE ..)\nTS_TEST(check:strict!)\nEND()\n")
        self.assertNotEqual(self.make("-t").returncode, 0)

    def test_relative_test_modules_use_the_tested_module_default_config(self):
        self.package("TS_PACKAGE")
        self.write("ya.make", (self.project / "ya.make").read_text() + "RECURSE_FOR_TESTS(tests)\n")
        self.write("src/answer.test.ts", "export const value = 42;\n")
        for module, config, content in (("TS_TEST_JEST_FOR", "jest.config.js", "module.exports = {};\n"), ("TS_TEST_VITEST_FOR", "vitest.config.ts", "export default {};\n")):
            with self.subTest(module=module):
                self.write(config, content)
                self.write("tests/ya.make", f"{module}(RELATIVE ..)\nTS_TEST_SRCS(../src)\nEND()\n")
                graph = self.graph("-t")
                tests = [node for node in graph["graph"] if any(path.endswith(".ts_test.fake") for path in node["outputs"])]
                self.assertEqual(len(tests), 1)
                self.assertIn(f"$(S)/{PROJECT}/{config}", tests[0]["inputs"])

    def test_json_style_and_script_checks_are_both_generated(self):
        self.package("TS_PACKAGE", "TS_LINT(check:strict!)\nSTYLE_JSON()")
        self.write("data.json", '{"answer":42}\n')
        graph = self.graph("-t")
        self.node(graph, "test-results/ts_lint_check_strict_/meta.json")
        styled = self.node(graph, "test-results/clang_format_json/meta.json")
        self.assertIn(f"$(S)/{PROJECT}/data.json", styled["inputs"])

    def test_explicit_test_modules_without_test_mode_have_no_run_node(self):
        self.package("TS_PACKAGE")
        for name in ("ya.make", "package.json", "pnpm-lock.yaml", "tsconfig.json", "src/answer.ts"):
            self.write("target/" + name, (self.project / name).read_text())
        self.write("ya.make", f"TS_TEST_FOR({PROJECT}/target)\nTS_TEST(check:strict!)\nEND()\n")
        result = self.make()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertNotIn("run_ts_check", json.dumps(json.loads(result.stdout)["graph"]))

    def test_single_character_exclusion_globs_match_one_character(self):
        self.package("TS_PACKAGE", "TS_EXCLUDE_FILES_GLOB(src/a?.ts)")
        for name in ("a1", "ab", "a12"):
            self.write(f"src/{name}.ts", "export const value = 42;\n")
        inputs = self.node(self.graph(), "output.tar")["inputs"]
        for name, included in (("a1", False), ("ab", False), ("a12", True)):
            self.assertEqual(f"$(S)/{PROJECT}/src/{name}.ts" in inputs, included, name)

    def test_checks_depend_on_workspace_proto_descriptions(self):
        self.package("TS_PACKAGE", "TS_LINT(check:strict!)")
        package = json.loads((self.project / "package.json").read_text())
        package["dependencies"] = {"@e2e/schema": "workspace:./schema"}
        self.write("package.json", json.dumps(package))
        self.write("schema/ya.make", "PROTO_SCHEMA()\nINCLUDE_TAGS(TS_PROTO_FROM_SCHEMA TS_PREPARE_DEPS)\nTS_PROTO_PACKAGE_NAME(@e2e/schema)\nSRCS(base.proto)\nEND()\n")
        self.write("schema/base.proto", 'syntax = "proto3"; package e2e; message Base { int32 value = 1; }\n')
        graph = self.graph("-t")
        checked = self.node(graph, "test-results/ts_lint_check_strict_/meta.json")
        descriptions = [node for node in graph["graph"] if any(path.startswith(f"$(B)/{PROJECT}/schema/") and path.endswith(".protodesc") for path in node["outputs"])]
        self.assertTrue(descriptions)
        self.assertTrue(any(node["uid"] in checked["deps"] for node in descriptions))

    def package(self, module="TS_TSC", macros=""):
        self.write("ya.make", f"{module}()\nTS_BUILD_OUTPUTS(dist)\n{macros}\nEND()\n")
        self.write("package.json", json.dumps({"name": "@e2e/check-options", "version": "1.0.0", "engines": {"node": "22.13.0"}, "files": ["src", "dist"], "scripts": {"nots:build": "tsc", "check:strict!": "true"}}))
        self.write("pnpm-lock.yaml", "lockfileVersion: '9.0'\nimporters:\n  .: {}\n")
        self.write("tsconfig.json", json.dumps({"compilerOptions": {"rootDir": "src", "outDir": "dist"}, "include": ["src/**/*.ts"]}))
        self.write(".eslintrc.js", "module.exports = {};\n")
        self.write("src/answer.ts", "export const answer = 42;\n")

    def test_medium_check_names_preserve_the_script_and_timeout(self):
        self.package("TS_PACKAGE", "TS_LINT(check:strict! TIMEOUT_MEDIUM)")
        graph = self.graph("-t", "--test-size=medium")
        checked = self.node(graph, "test-results/ts_lint_check_strict_/meta.json")
        args = [arg for command in checked["cmds"] for arg in command["cmd_args"]]
        self.assertEqual(args[args.index("--script-name") + 1], "check:strict!")
        self.assertEqual(args[args.index("--timeout") + 1], "600")

    def test_hermetic_checks_extract_the_node_modules_layer(self):
        self.package("TS_PACKAGE", "WITH_NODE_MODULES()\nTS_LINT(check:strict!)")
        checked = self.node(self.graph("-t", "-DAUTOCHECK=yes"), "test-results/ts_lint_check_strict_/meta.json")
        args = [arg for command in checked["cmds"] for arg in command["cmd_args"]]
        recipes = base64.b64decode(args[args.index("--recipes") + 1]).decode()
        self.assertIn("extract-node-modules", recipes)
        self.assertIn("node_modules.layer", recipes)

    def test_legacy_stylelint_and_biome_checks_are_generated(self):
        self.package(macros="TS_STYLELINT(stylelint.config.js)\nTS_BIOME(biome.json)")
        self.write("stylelint.config.js", "module.exports = {};\n")
        self.write("biome.json", "{}\n")
        self.write("src/style.css", "body { color: blue; }\n")
        graph = self.graph("-t")
        for suite, command in (("ts_stylelint", "run_stylelint"), ("ts_biome", "run_biome")):
            with self.subTest(suite=suite):
                args = [arg for cmd in self.node(graph, f"test-results/{suite}/meta.json")["cmds"] for arg in cmd["cmd_args"]]
                self.assertIn(command, args)

    def test_legacy_typecheck_uses_the_default_tsconfig(self):
        self.package(macros="TS_TYPECHECK()")
        checked = self.node(self.graph("-t"), "test-results/tsc_typecheck/meta.json")
        args = [arg for command in checked["cmds"] for arg in command["cmd_args"]]
        self.assertIn("run_tsc_typecheck", args)
        self.assertEqual(args[args.index("--ts-config-path") + 1], "tsconfig.json")

    def test_legacy_checks_without_matching_sources_have_no_suite(self):
        self.package(macros="TS_STYLELINT(stylelint.config.js)")
        self.write("stylelint.config.js", "module.exports = {};\n")
        graph = self.graph("-t")
        self.node(graph, "test-results/eslint/meta.json")
        self.assertFalse(any(path.endswith("/ts_stylelint/meta.json") for node in graph["graph"] for path in node["outputs"]))
