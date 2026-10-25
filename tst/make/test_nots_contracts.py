import json
import unittest

from tst import lib
from tst.make import test_nots
from tst.make.test_java_tests import option_values, run_args


class NotsContractsTest(unittest.TestCase):
    def test_node_version_constraints_select_the_lowest_matching_resource(self):
        for constraint, selected in ((">=22.19.0", "22.20.0"), ("=24.1.0", "24.1.0"), ("22.18.0", "22.18.0")):
            with self.subTest(constraint=constraint):
                files = test_nots.NotsTests().project()
                files["web/package.json"] = json.dumps({"engines": {"node": constraint}})
                for version in ("22.20.0", "24.1.0"):
                    files[f"build/platform/nodejs/{version}/ya.make"] = lib.resource_lib("NODEJS_" + version.replace(".", "_"), 7)
                graph = lib.make(files, "web")
                prepare = lib.node_by_output(graph, "$(B)/web/package.json")
                args = prepare["cmds"][0]["cmd_args"]
                self.assertTrue(any(f"resources/NODEJS_{selected.replace('.', '_')}/" in arg for arg in args), args)

    def test_invalid_workspace_and_missing_script_are_diagnosed(self):
        for macro, manifest, message in (
            ("TS_PACKAGE()", '{"dependencies":{"bad":"workspace:*"}}', "expected relative workspace dependency"),
            ("TS_PACKAGE()\nTS_LINT()", "{}", "TS_LINT requires a script name"),
            ("TS_PACKAGE()\nTS_TEST()", "{}", "TS_TEST requires a script name"),
        ):
            with self.subTest(macro=macro, manifest=manifest):
                files = test_nots.NotsTests().project(macro)
                files["web/package.json"] = manifest
                result = lib.make_process(files, "web")
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(message, result.stderr)
                self.assertNotIn("panic:", result.stderr)

    def test_next_options_and_recursive_suffix_exclusions(self):
        files = test_nots.NotsTests().project("TS_NEXT()\nTS_NEXT_BUILD_OPTIONS(--debug)\nTS_EXCLUDE_FILES_GLOB(cache**)")
        files.update({"web/cache.js": "", "web/cache/nested/a.js": "", "web/src/keep.js": ""})
        bundle = lib.node_by_output(lib.make(files, "web"), "$(B)/web/output.tar")
        self.assertIn("next build --debug", bundle["cmds"][-1]["cmd_args"])
        self.assertIn("$(S)/web/src/keep.js", bundle["inputs"])
        self.assertNotIn("$(S)/web/cache.js", bundle["inputs"])
        self.assertNotIn("$(S)/web/cache/nested/a.js", bundle["inputs"])

    def test_inferred_tarball_urls_share_the_registry_configuration(self):
        files = test_nots.NotsTests().project()
        files["web/pnpm-lock.yaml"] = json.dumps({"lockfileVersion": "9.0", "packages": {
            "first@1.0.0": {"resolution": {"integrity": "sha1-YWJj"}},
            "second@2.0.0": {"resolution": {"integrity": "sha1-YWJj"}},
        }})
        prepare = lib.node_by_output(lib.make(files, "web"), "$(B)/web/package.json")
        self.assertEqual(prepare["resources"], [
            {"uri": "https://npm.yandex-team.ru/first/-/first-1.0.0.tgz#integrity=sha1-YWJj"},
            {"uri": "https://npm.yandex-team.ru/second/-/second-2.0.0.tgz#integrity=sha1-YWJj"},
        ])

    def test_custom_check_command_and_python_resource_reach_the_runner(self):
        files = test_nots.NotsTests().project("TS_PACKAGE()\nTS_LINT(lint custom --check)")
        files["build/platform/python/ymake_python3/ya.make"] = lib.resource_lib("YMAKE_PYTHON3", 8)
        graph = lib.make(files, "web", "-t")
        check = lib.node_by_output(graph, "$(B)/web/test-results/ts_lint_lint/meta.json")
        self.assertEqual(option_values(run_args(check), "--command"), ["custom --check"])
        self.assertIn("YMAKE_PYTHON3_RESOURCE_GLOBAL::$(B)/resources/YMAKE_PYTHON3",
                      option_values(check["cmds"][-1]["cmd_args"], "--global-resource"))

    def test_import_scanner_handles_unfinished_comments_and_escaped_strings(self):
        files = test_nots.NotsTests().project()
        files.update({
            "web/tests/ya.make": "TS_TEST_VITEST_FOR(web)\nTS_TEST_SRCS(../src)\nEND()\n",
            "web/src/unit.test.ts": "import './line';\nimport './block';\nconst text = \"escaped\\\"quote\";\n",
            "web/src/line.ts": "// comment without newline",
            "web/src/block.ts": "/* unfinished comment",
        })
        graph = lib.make(files, "web/tests", "-t")
        test = lib.node_by_output(graph, "$(B)/web/tests/web-tests.ts_test.fake")
        for name in ("unit.test.ts", "line.ts", "block.ts"):
            self.assertIn("$(S)/web/src/" + name, test["inputs"])
        self.assertFalse(any("absent" in name for name in test["inputs"]))
        files["web/src/unit.test.ts"] += "import './absent';\n"
        result = lib.make_process(files, "web/tests", "-t")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("unresolved include <./absent>", result.stderr)
        self.assertNotIn("panic:", result.stderr)


if __name__ == "__main__":
    unittest.main(verbosity=2)
