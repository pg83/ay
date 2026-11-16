import json

from tst.make.lib import MakeTest, nodejs_version


class NotsConfigureErrorsTest(MakeTest):
    """Configure errors of the TypeScript plugin (build/plugins/nots.py) and its _TS_CONF_ERROR conf macros."""

    def ts_module(self, module, *statements, files=("src", "dist"), path=""):
        prefix = path + "/" if path else ""
        self.write(prefix + "ya.make", f"{module}()\n" + "".join(statement + "\n" for statement in statements) + "END()\n")
        self.write(prefix + "package.json", json.dumps({"name": "@e2e/" + (path or "answer"), "version": "1.0.0", "engines": {"node": nodejs_version()}, "files": list(files), "scripts": {"nots:build": "tsc", "test": "jest"}}))
        self.write(prefix + "pnpm-lock.yaml", "lockfileVersion: '9.0'\nsettings:\n  autoInstallPeers: true\n  excludeLinksFromLockfile: false\nimporters:\n  .: {}\n")
        self.write(prefix + "tsconfig.json", json.dumps({"compilerOptions": {"rootDir": "src", "outDir": "dist", "target": "ES2022"}, "include": ["src/**/*.ts"]}))
        self.write(prefix + "src/answer.ts", "export const answer = 42;\n")

    def library(self, *statements, **options):
        self.ts_module("TS_LIBRARY", *statements, **options)

    def parent_library(self):
        self.library("TS_BUILD_SCRIPT(nots:build)", "TS_BUILD_OUTPUTS(dist)", path="lib")

    def test_library_without_a_build_script(self):
        self.library("TS_BUILD_OUTPUTS(dist)")
        self.assertConfigureLikeYa()

    def test_library_without_outputs(self):
        self.library("TS_BUILD_SCRIPT(nots:build)")
        self.assertConfigureLikeYa()

    def test_library_outputs_missing_in_package_files(self):
        self.library("TS_BUILD_SCRIPT(nots:build)", "TS_BUILD_OUTPUTS(out)")
        self.assertConfigureLikeYa()

    def test_build_env_without_a_value(self):
        self.library("TS_BUILD_SCRIPT(nots:build)", "TS_BUILD_OUTPUTS(dist)", "SET(TS_BUILD_ENV MODE)")
        self.assertConfigureLikeYa()

    def module_tests(self, *statements):
        self.parent_library()
        self.write("tests/ya.make", "".join(statement + "\n" for statement in statements) + "END()\n")
        self.write("ya.make", "RECURSE(lib tests)\n")

    def test_test_for_without_a_test_script(self):
        self.module_tests(f"TS_TEST_FOR({self.project_name}/lib)")
        self.assertConfigureLikeYa()

    def test_test_for_a_relative_path_without_relative(self):
        self.module_tests("TS_TEST_FOR(../lib)", "TS_TEST(jest)")
        self.assertConfigureLikeYa()

    def test_large_files_without_a_destination(self):
        self.ts_module("TS_PACKAGE", "TS_LARGE_FILES(large.bin)")
        self.write("large.bin.external", '{"storage": "SANDBOX", "resource_id": 123456789}\n')
        self.assertConfigureLikeYa()

    def test_unit_tests_in_a_library(self):
        self.library("TS_BUILD_SCRIPT(nots:build)", "TS_BUILD_OUTPUTS(dist)", "TS_TEST(jest)")
        self.assertConfigureLikeYa()

    def test_files_in_a_library(self):
        self.library("TS_BUILD_SCRIPT(nots:build)", "TS_BUILD_OUTPUTS(dist)", "TS_FILES(src/answer.ts)")
        self.assertConfigureLikeYa()

    def test_test_config_in_test_for(self):
        self.module_tests(f"TS_TEST_FOR({self.project_name}/lib)", "TS_TEST(jest)", "TS_TEST_CONFIG(jest.config.js)")
        self.assertConfigureLikeYa()

    def test_test_srcs_in_test_for(self):
        self.module_tests(f"TS_TEST_FOR({self.project_name}/lib)", "TS_TEST(jest)", "TS_TEST_SRCS(src)")
        self.assertConfigureLikeYa()

    def test_typecheck_in_a_library(self):
        self.library("TS_BUILD_SCRIPT(nots:build)", "TS_BUILD_OUTPUTS(dist)", "TS_TYPECHECK()")
        self.assertConfigureLikeYa()

    def test_after_build_script_in_a_library(self):
        self.library("TS_BUILD_SCRIPT(nots:build)", "TS_BUILD_OUTPUTS(dist)", "RUN_JAVASCRIPT_AFTER_BUILD(after.js)")
        self.write("after.js", "console.log(42);\n")
        self.assertConfigureLikeYa()

    def test_config_in_a_library(self):
        self.library("TS_BUILD_SCRIPT(nots:build)", "TS_BUILD_OUTPUTS(dist)", "TS_CONFIG(tsconfig.json)")
        self.assertConfigureLikeYa()
