import json
import unittest

import lib
import test_nots


CASES = {
    "plain": "",
    "simple": "RUN_JAVASCRIPT_AFTER_BUILD(tools/post.js)\n",
    "arguments": 'RUN_JAVASCRIPT_AFTER_BUILD(tools/post.js --mark "two words" IN a.yaml OUTDIR extra)\n',
    "script_before": "TS_BUILD_SCRIPT(custom custom build)\nRUN_JAVASCRIPT_AFTER_BUILD(tools/post.js --mark)\n",
    "script_after": "RUN_JAVASCRIPT_AFTER_BUILD(tools/post.js --mark)\nTS_BUILD_SCRIPT(custom custom build)\n",
    "repeated": "RUN_JAVASCRIPT_AFTER_BUILD(tools/first.js OUTDIR extra)\nRUN_JAVASCRIPT_AFTER_BUILD(tools/post.js last OUTDIR second)\n",
    "reset_outputs": "RUN_JAVASCRIPT_AFTER_BUILD(tools/post.js OUTDIR extra)\nTS_BUILD_OUTPUTS(dist)\n",
    "variables": "SET(SCRIPT tools/post.js)\nSET(FLAG first)\nRUN_JAVASCRIPT_AFTER_BUILD(${SCRIPT} $FLAG)\nSET(SCRIPT tools/first.js)\nSET(FLAG last)\n",
}


def fixture(cases=CASES):
    files = test_nots.NotsTests().project()
    files = {key: value for key, value in files.items() if not key.startswith("web/")}
    files["devtools/frontend_build_platform/nots/constants/src/tier0-settings.yaml"] = "pnpm:\n  supportedCustomSettings: {}\n  defaults:\n    registry: https://npm.yandex-team.ru\n"
    roots = []
    for name, body in cases.items():
        root = "garden/" + name
        roots.append(root)
        files[root + "/ya.make"] = "TS_TSC()\nNO_LINT()\nTS_BUILD_OUTPUTS(dist)\n" + body + "END()\n"
        files[root + "/package.json"] = json.dumps({"name": name, "version": "1.0.0", "files": ["dist", "extra", "second"], "packageManager": "pnpm@10.0.0"})
        files[root + "/pnpm-lock.yaml"] = "lockfileVersion: '9.0'\nimporters:\n  .: {}\npackages: {}\n"
        files[root + "/tsconfig.json"] = json.dumps({"compilerOptions": {"outDir": "dist", "rootDir": "src"}, "include": ["src"]})
        files[root + "/src/index.ts"] = "export const value = 1;\n"
        files[root + "/tools/post.js"] = "console.log('post');\n"
        files[root + "/tools/first.js"] = "console.log('first');\n"
        files[root + "/a.yaml"] = "excluded metadata\n"
    return files, roots


class NotsAfterBuildTest(unittest.TestCase):
    def graph(self, cases):
        files, roots = fixture({name: CASES[name] for name in cases})
        graph = lib.make(files, *roots, "--sandboxing", "--target-platform", "default-linux-x86_64")
        ids = {node["uid"] for node in graph["graph"]}
        self.assertTrue(graph["result"])
        self.assertLessEqual(set(graph["result"]), ids)
        self.assertTrue(all(set(node["deps"]) <= ids for node in graph["graph"]))
        return graph

    def built(self, graph, name):
        prefix = "$(B)/garden/" + name
        node = lib.node_by_output(graph, prefix + "/output.tar")
        self.assertEqual(node["outputs"], [prefix + "/output.tar", prefix + "/output.tar.uuid"])
        self.assertIn(node["uid"], graph["result"])
        self.assertEqual(len(node["cmds"]), 2)
        self.assertEqual(node["cmds"][0]["cwd"], prefix)
        self.assertEqual(node["cmds"][0]["cmd_args"][1:], ["$(S)/build/scripts/autotar_gendirs.py", "--unpack", "--ext", ".gentar"])
        self.assertNotIn("cwd", node["cmds"][1])
        prepare = lib.node_by_output(graph, prefix + "/package.json")
        self.assertIn(prepare["uid"], node["deps"])
        self.assertIn(prefix + "/package.json", node["inputs"])
        self.assertNotIn("--build-command", prepare["cmds"][0]["cmd_args"])
        self.assertNotIn("$(S)/garden/" + name + "/a.yaml", node["inputs"])
        return node, node["cmds"][1]["cmd_args"]

    def test_absent_and_simple_postbuild_keep_order_cwd_and_real_inputs(self):
        graph = self.graph(("plain", "simple"))
        base = "tsc --project tsconfig.json --incremental false --composite false --pretty"
        for name, suffix in (("plain", ""), ("simple", " && node tools/post.js")):
            with self.subTest(name=name):
                node, argv = self.built(graph, name)
                self.assertEqual(argv[argv.index("--build-command") + 1], base + suffix)
                self.assertIn("$(S)/garden/" + name + "/tools/post.js", node["inputs"])

    def test_named_arguments_append_outputs_and_ignore_unused_in(self):
        graph = self.graph(("arguments",))
        _, argv = self.built(graph, "arguments")
        self.assertEqual(argv[argv.index("--outputs") + 1:argv.index("--exclude-globs")], ["dist", "extra"])
        self.assertTrue(argv[-1].endswith(" && node tools/post.js --mark two words"))
        self.assertNotIn("--after-build-js", argv)

    def test_source_order_keeps_last_postcommand_and_final_build_command(self):
        graph = self.graph(("script_before", "script_after", "repeated", "reset_outputs"))
        for name in ("script_before", "script_after"):
            with self.subTest(name=name):
                _, argv = self.built(graph, name)
                self.assertEqual(argv[argv.index("--build-script") + 1], "custom")
                self.assertEqual(argv[-1], "tsc --project tsconfig.json --incremental false --composite false --pretty && node tools/post.js --mark")
        _, argv = self.built(graph, "repeated")
        self.assertTrue(argv[-1].endswith(" && node tools/post.js last"))
        self.assertNotIn("node tools/first.js", argv[-1])
        self.assertEqual(argv[argv.index("--outputs") + 1:argv.index("--exclude-globs")], ["dist", "extra", "second"])
        _, argv = self.built(graph, "reset_outputs")
        self.assertTrue(argv[-1].endswith(" && node tools/post.js"))
        self.assertEqual(argv[argv.index("--outputs") + 1:argv.index("--exclude-globs")], ["dist"])


    def test_macro_arguments_freeze_at_call(self):
        graph = self.graph(("variables",))
        _, argv = self.built(graph, "variables")
        self.assertTrue(argv[-1].endswith(" && node tools/post.js first"))

    def test_prepare_context_ignores_even_missing_macro_arguments(self):
        files, _ = fixture({"simple": CASES["simple"]})
        files["garden/simple/ya.make"] = "TS_TSC()\nNO_LINT()\nTS_BUILD_OUTPUTS(dist)\nRUN_JAVASCRIPT_AFTER_BUILD()\nEND()\n"
        files["garden/simple/tests/ya.make"] = "TS_TEST_FOR(garden/simple)\nTS_TEST(test)\nNO_LINT()\nEND()\n"
        graph = lib.make(files, "garden/simple/tests", "--sandboxing")
        lib.node_by_output(graph, "$(B)/garden/simple/package.json")
        self.assertFalse(any("$(B)/garden/simple/output.tar" in node["outputs"] for node in graph["graph"]))

    def test_build_rejects_missing_script_and_unsupported_modern_modules(self):
        files, _ = fixture({"simple": CASES["simple"]})
        for declaration, invocation, expected in (
            ("TS_TSC", "RUN_JAVASCRIPT_AFTER_BUILD()", "requires a script"),
            ("TS_PACKAGE", "RUN_JAVASCRIPT_AFTER_BUILD(tools/post.js)", "does not support"),
            ("TS_LIBRARY", "RUN_JAVASCRIPT_AFTER_BUILD(tools/post.js)", "does not support"),
        ):
            with self.subTest(declaration=declaration):
                files["garden/simple/ya.make"] = declaration + "()\nNO_LINT()\nTS_BUILD_SCRIPT(build)\nTS_BUILD_OUTPUTS(dist)\n" + invocation + "\nEND()\n"
                result = lib.make_process(files, "garden/simple", "--sandboxing")
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(expected, result.stderr)


    def test_postcommand_end_snapshot_respects_set_clear_and_directory_setters(self):
        for value in ("", "node tools/first.js overridden"):
            with self.subTest(value=value):
                files, _ = fixture({"simple": CASES["simple"]})
                files["garden/simple/ya.make"] = files["garden/simple/ya.make"].replace(
                    "END()", "SET(_TS_AFTER_BUILD_COMMAND " + value + ")\nEND()\nSET(_TS_AFTER_BUILD_COMMAND node directory.js)")
                graph = lib.make(files, "garden/simple", "--sandboxing")
                _, argv = self.built(graph, "simple")
                base = "tsc --project tsconfig.json --incremental false --composite false --pretty"
                self.assertEqual(argv[-1], base + (" && " + value if value else ""))

    def test_missing_required_script_in_other_consumer_is_rejected(self):
        files, _ = fixture({"simple": CASES["simple"]})
        files["garden/simple/tests/ya.make"] = "TS_TEST_FOR(garden/simple)\nRUN_JAVASCRIPT_AFTER_BUILD()\nEND()\n"
        result = lib.make_process(files, "garden/simple/tests", "--sandboxing")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("RUN_JAVASCRIPT_AFTER_BUILD", result.stderr)


if __name__ == "__main__":
    unittest.main()
