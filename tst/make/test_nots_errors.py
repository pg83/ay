"""Validation errors and optional resources in the TypeScript toolchain."""
import json
import unittest

from tst import lib


def project():
    files = {
        "web/ya.make": "TS_PACKAGE()\nEND()\n",
        "web/package.json": '{"name":"web","dependencies":{"dep":"1.2.3"}}',
        "web/pnpm-lock.yaml": "lockfileVersion: '9.0'\npackages: {}\n",
        "devtools/frontend_build_platform/erm/erm-packages.json": json.dumps({
            "nodejs": {"default": "22.18.0", "versions": {"22.18.0": {}}},
        }),
        "devtools/frontend_build_platform/nots/constants/src/tier0-settings.yaml": "pnpm:\n  defaults:\n    registry: https://npm.yandex-team.ru\n",
        "build/platform/nodejs/22.18.0/ya.make": lib.resource_lib("NODEJS_22_18_0", 1),
        "build/external_resources/pnpm/ya.make": lib.resource_lib("PNPM", 2),
    }
    lib.tool_program(files, "devtools/frontend_build_platform/nots/builder", "builder")
    return files


class NotsValidationTest(unittest.TestCase):
    def assert_error(self, files, message):
        result = lib.make_process(files, "web")
        self.assertEqual(result.returncode, 1, result.stderr)
        self.assertIn(message, result.stderr)
        self.assertNotIn("panic:", result.stderr)

    def test_invalid_node_versions(self):
        for default, versions, constraint, message in (
            ("", {}, "", "no default version"),
            ("18.0.0", {}, "", "Node.js 18.0.0 is unsupported"),
            ("22.18.0", {}, "^22", "unsupported version range"),
            ("22.18.0", {}, "23.0.0", "no nodejs version satisfies"),
            ("22.18.0", {"25.1": {}}, ">=22.0.0", "invalid stable version"),
            ("22.18.0", {"25.x.0": {}}, ">=22.0.0", "invalid syntax"),
        ):
            with self.subTest(default=default, constraint=constraint, versions=versions):
                files = project()
                files["web/package.json"] = json.dumps({"engines": {"node": constraint}})
                files["devtools/frontend_build_platform/erm/erm-packages.json"] = json.dumps({
                    "nodejs": {"default": default, "versions": versions},
                })
                self.assert_error(files, message)

    def test_invalid_lockfile_records(self):
        for version, name, resolution, message in (
            ("5.0", "dep@1.2.3", {}, "unsupported lockfileVersion"),
            ("9.0", "invalid", {"integrity": "sha1-YWJj"}, "cannot infer tarball URL"),
            ("9.0", "dep@1.2.3", {"tarball": "file:../dep.tgz"}, "tarball cannot point to a file"),
            ("9.0", "dep@1.2.3", {"tarball": "https://example.org/dep.tgz"}, "tarball must use npm.yandex-team.ru"),
            ("9.0", "dep@1.2.3", {"integrity": "bad"}, "invalid integrity"),
            ("9.0", "dep@1.2.3", {"integrity": "sha256-YWJj"}, "invalid integrity"),
            ("9.0", "dep@1.2.3", {"integrity": "sha512-%%%"}, "illegal base64"),
            ("9.0", "dep@1.2.3", {"integrity": "sha1-YWJj", "tarball": "dep/%zz.tgz"}, "invalid URL escape"),
        ):
            with self.subTest(version=version, resolution=resolution):
                files = project()
                # JSON is also valid YAML and preserves each malformed scalar.
                files["web/pnpm-lock.yaml"] = json.dumps({
                    "lockfileVersion": version, "packages": {name: {"resolution": resolution}},
                })
                self.assert_error(files, message)

    def test_missing_resource_is_diagnosed(self):
        files = project()
        files["build/external_resources/pnpm/ya.make"] = lib.resource_lib("OTHER", 2)
        self.assert_error(files, "did not declare resource PNPM")

    def test_relative_tarball_without_package_separator(self):
        files = project()
        files["web/pnpm-lock.yaml"] = json.dumps({
            "lockfileVersion": "9.0", "packages": {"dep@1.2.3": {"resolution": {
                "integrity": "sha1-YWJj", "tarball": "cache/dep/archive.tgz",
            }}},
        })
        prepare = lib.node_by_output(lib.make(files, "web"), "$(B)/web/package.json")
        self.assertIn("$(B)/web/__tarballs__/cache/dep/archive.tgz", prepare["outputs"])
        self.assertEqual(prepare["resources"], [{
            "uri": "https://npm.yandex-team.ru/cache/dep/archive.tgz#integrity=sha1-YWJj",
        }])

    def test_local_cli_configs_and_workspace_metadata(self):
        files = project()
        files["web/ya.make"] = """TS_TSC()
TS_CONFIG(one.json two.json)
TS_EXCLUDE_FILES_GLOB(skip?.js)
DISABLE_HERMETIC_NODE_MODULES()
END()
"""
        files["web/package.json"] = json.dumps({
            "dependencies": {"dep": "1.2.3"},
            "nots": {"commonConfigPath": "common.json"},
            "pnpm": {"patchedDependencies": {"dep@1.2.3": "patches/dep.patch"}},
        })
        files.update({"web/common.json": "{}", "web/patches/dep.patch": "patch", "web/skip1.js": "skip", "web/keep.js": "keep"})
        graph = lib.make(files, "web", "-DTS_LOCAL_CLI=yes")
        prepare = lib.node_by_output(graph, "$(B)/web/package.json")
        self.assertIn("$(S)/web/common.json", prepare["inputs"])
        self.assertNotIn("$(S)/web/patches/dep.patch", prepare["inputs"])
        bundle = lib.node_by_output(graph, "$(B)/web/output.tar")
        args = bundle["cmds"][-1]["cmd_args"]
        self.assertEqual(args[args.index("--local-cli") + 1], "yes")
        self.assertIn("tsc --project one.json --incremental false --composite false --pretty && tsc --project two.json --incremental false --composite false --pretty", args)
        self.assertIn("$(S)/web/keep.js", bundle["inputs"])
        self.assertNotIn("$(S)/web/skip1.js", bundle["inputs"])
        files["web/ya.make"] = files["web/ya.make"].replace("DISABLE_HERMETIC_NODE_MODULES()", "")
        hermetic = lib.node_by_output(lib.make(files, "web", "-DAUTOCHECK=yes"), "$(B)/web/package.json")
        self.assertIn("$(S)/web/patches/dep.patch", hermetic["inputs"])


    def test_test_for_can_request_the_tested_build(self):
        files = project()
        files["web/ya.make"] = "TS_NEXT()\nEND()\n"
        files["web/index.js"] = "module.exports = 1;\n"
        files["web/tests/ya.make"] = "TS_TEST_FOR(web)\nTS_TEST(test custom --flag TIMEOUT_MEDIUM)\nTS_TEST_DEPENDS_ON_BUILD()\nEND()\n"
        graph = lib.make(files, "web/tests", "--target-platform-tests")
        built = lib.node_by_output(graph, "$(B)/web/output.tar")
        tested = lib.node_by_output(graph, "$(B)/web/tests/web-tests")
        self.assertNotIn(built["uid"], tested["deps"])
        self.assertNotIn("$(B)/web/output.tar", tested["inputs"])
        execution = lib.node_by_output(graph, "$(B)/web/tests/test-results/ts_test_test/meta.json")
        self.assertIn(built["uid"], execution["deps"])
        self.assertNotIn("$(B)/web/output.tar", execution["inputs"])
        self.assertIn("next build", built["cmds"][-1]["cmd_args"])

    def test_test_for_requires_a_module_path(self):
        for argument in ("", ".", "./", "RELATIVE ../web"):
            with self.subTest(argument=argument):
                files = project()
                files["web/ya.make"] = f"TS_TEST_FOR({argument})\nEND()\n"
                self.assert_error(files, "requires a tested module path")

    def test_node25_prebuilder_and_production_bundle(self):
        files = project()
        files["web/ya.make"] = """TS_VITE()
TS_VITE_CONFIG(first.ts second.ts)
TS_BUILD_OUTPUTS(dist)
WITH_NODE_MODULES(PROD)
TS_OUTPUT_PREFIX()
VCS_INFO_FILE(FILE version.json)
END()
"""
        files["web/package.json"] = '{"usePrebuilder":true,"dependencies":{"dep":"1.2.3"}}'
        files["web/dist/generated.js"] = "generated"
        files["web/source.js"] = "source"
        files["devtools/frontend_build_platform/erm/erm-packages.json"] = json.dumps({
            "nodejs": {"default": "25.1.0"}, "@yatool/prebuilder": {"default": "1.2.0"},
        })
        for directory, name in (
            ("build/platform/nodejs/25.1.0", "NODEJS_25_1_0"),
            ("build/platform/nodejs/libatomic", "LIBATOMIC_1_2_0"),
            ("build/external_resources/yatool_prebuilder/1.2.0", "YATOOL_PREBUILDER_1_2_0"),
        ):
            files[f"{directory}/ya.make"] = lib.resource_lib(name, 3)
        graph = lib.make(files, "web", "--target-platform", "default-linux-x86_64")
        bundle = lib.node_by_output(graph, "$(B)/web/output.tar")
        args = bundle["cmds"][-1]["cmd_args"]
        self.assertIn("--ld-library-path", args)
        self.assertIn("--yatool-prebuilder-path", args)
        self.assertIn("--nm-bundle-prod", args)
        self.assertIn("--output-prefix=web", args)
        self.assertIn("vite build --config first.ts && vite build --config second.ts", args)
        self.assertIn("$(B)/web/workspace_node_modules.tar", bundle["outputs"])
        self.assertIn("$(B)/web/version.json", bundle["outputs"])
        self.assertIn("$(S)/web/source.js", bundle["inputs"])
        self.assertNotIn("$(S)/web/dist/generated.js", bundle["inputs"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
