import json

from tst.make.lib import MakeTest, PROJECT, nodejs_version


class MiscConfigureErrorsTest(MakeTest):
    """Configure errors of devtools/ymake (add_iter.cpp, module_builder.cpp, module_confs.cpp, module_dir.cpp,
    main.cpp, makefile_loader.cpp) and of ytest.py, gobuild.py and _dart_fields.py not covered elsewhere."""

    def library(self, *statements):
        self.write("ya.make", "LIBRARY()\nSRCS(value.cpp)\n" + "".join(statement + "\n" for statement in statements) + "END()\n")
        self.write("value.cpp", "int value() { return 42; }\n")

    def unittest(self, *statements):
        self.write("ya.make", "UNITTEST()\nSRCS(test.cpp)\n" + "".join(statement + "\n" for statement in statements) + "END()\n")
        self.write("test.cpp", "int answer() { return 42; }\n")

    def test_peerdir_to_a_multimodule_excluding_the_tag(self):
        self.write("ya.make", f"GO_LIBRARY()\nSRCS(value.go)\nPEERDIR({PROJECT}/proto)\nEND()\n")
        self.write("value.go", "package value\n")
        self.write("proto/ya.make", "PROTO_LIBRARY()\nSRCS(value.proto)\nEXCLUDE_TAGS(GO_PROTO)\nEND()\n")
        self.write("proto/value.proto", "syntax = \"proto3\";\nmessage Value {}\n")
        self.assertConfigureLikeYa()

    def test_srcs_with_a_directory(self):
        self.library("SRCS(sub)")
        self.write("sub/value.cpp", "int sub() { return 42; }\n")
        self.assertConfigureLikeYa()

    def test_dll_with_exports_in_the_header(self):
        self.write("ya.make", "DLL(answer EXPORTS answer.exports)\nSRCS(value.cpp)\nEND()\n")
        self.write("value.cpp", "int value() { return 42; }\n")
        self.write("answer.exports", "C value\n")
        self.assertConfigureLikeYa()

    def test_depends_on_a_directory_without_modules(self):
        self.unittest(f"DEPENDS({PROJECT}/sub)")
        self.write("sub/ya.make", "RECURSE()\n")
        self.assertConfigureLikeYa()

    def test_depends_on_a_multimodule_without_final_targets(self):
        self.unittest(f"DEPENDS({PROJECT}/proto)")
        self.write("proto/ya.make", "PROTO_LIBRARY()\nSRCS(value.proto)\nEND()\n")
        self.write("proto/value.proto", "syntax = \"proto3\";\nmessage Value {}\n")
        self.assertConfigureLikeYa()

    def test_autoinclude_without_linters_make_inc(self):
        core = self.root / "build/ymake.core.conf"
        self.write_tree("build/ymake.core.conf", core.read_text() + f"\nAUTOINCLUDE_PATHS+={PROJECT}/autoincludes.json\n")
        self.write("autoincludes.json", json.dumps([PROJECT]))
        self.library()
        self.assertConfigureLikeYa()

    def test_jtest_for_without_arguments(self):
        self.write("ya.make", "JTEST_FOR()\nJDK_VERSION(17)\nEND()\n")
        self.assertConfigureLikeYa()

    def test_go_resource_key_without_a_value(self):
        self.write("ya.make", "GO_LIBRARY()\nSRCS(value.go)\nRESOURCE(- E2E)\nEND()\n")
        self.write("value.go", "package value\n")
        self.assertConfigureLikeYa()

    def test_disallowed_macro_in_linters_make_inc(self):
        core = self.root / "build/ymake.core.conf"
        self.write_tree("build/ymake.core.conf", core.read_text() + f"\nAUTOINCLUDE_PATHS+={PROJECT}/autoincludes.json\n")
        self.write("autoincludes.json", json.dumps([PROJECT]))
        self.write("linters.make.inc", "PEERDIR(util)\n")
        self.library()
        self.assertConfigureLikeYa()

    def test_peerdir_with_a_trailing_slash(self):
        self.library(f"PEERDIR({PROJECT}/sub/)")
        self.write("sub/ya.make", "LIBRARY()\nSRCS(sub.cpp)\nEND()\n")
        self.write("sub/sub.cpp", "int sub() { return 42; }\n")
        self.assertConfigureLikeYa()

    def test_default_module_license_that_is_invalid(self):
        core = self.root / "build/ymake.core.conf"
        self.write_tree("build/ymake.core.conf", core.read_text() + "\nDEFAULT_MODULE_LICENSE=MIT OR\n")
        self.library()
        self.assertConfigureLikeYa()

    def subdir(self):
        self.write("sub/value.txt", "value\n")

    def test_copy_file_of_a_directory(self):
        self.library("COPY_FILE(sub sub.copy)")
        self.subdir()
        self.assertConfigureLikeYa()

    def test_run_python_with_a_directory_input(self):
        self.library("RUN_PYTHON3(gen.py IN sub OUT out.txt)")
        self.write("gen.py", "print(42)\n")
        self.subdir()
        self.assertConfigureLikeYa()

    def test_run_program_with_a_directory_input(self):
        self.library(f"RUN_PROGRAM({PROJECT}/tool IN sub STDOUT out.txt)")
        self.write("tool/ya.make", "PROGRAM()\nSRCS(main.cpp)\nEND()\n")
        self.write("tool/main.cpp", "int main() { return 0; }\n")
        self.subdir()
        self.assertConfigureLikeYa()

    def test_resource_uri_from_a_missing_json(self):
        self.library("SET_RESOURCE_URI_FROM_JSON(E2E_URI missing.json)")
        self.assertConfigureLikeYa()

    def test_resource_uri_from_an_invalid_json(self):
        self.library("SET_RESOURCE_URI_FROM_JSON(E2E_URI resource.json)")
        self.write("resource.json", "{broken\n")
        self.assertConfigureLikeYa()

    def test_resource_uri_from_json_with_one_argument(self):
        self.library("SET_RESOURCE_URI_FROM_JSON(E2E_URI)")
        self.assertConfigureLikeYa()

    def test_external_resource_by_json_without_the_platform(self):
        self.library("DECLARE_EXTERNAL_RESOURCE_BY_JSON(E2E_RESOURCE resource.json)")
        self.write("resource.json", json.dumps({"by_platform": {"win32-x86_64": {"uri": "sbr:1"}}}))
        self.assertConfigureLikeYa()

    def test_unreadable_source(self):
        self.library()
        self.write("value.cpp", '#include "value.h"\nint value() { return 42; }\n')
        self.write("value.h", "#pragma once\n")
        (self.project / "value.h").chmod(0)
        self.addCleanup((self.project / "value.h").chmod, 0o644)
        self.assertConfigureLikeYa()

    def ts_imports(self, source):
        self.write("ya.make", "TS_PACKAGE()\nEND()\nRECURSE_FOR_TESTS(tests)\n")
        self.write("package.json", json.dumps({"name": "@e2e/imports", "version": "1.0.0", "engines": {"node": nodejs_version()}}))
        self.write("pnpm-lock.yaml", "lockfileVersion: '9.0'\nimporters:\n  .: {}\n")
        self.write("tsconfig.json", '{"compilerOptions":{"target":"ES2022"},"include":["src/**/*.ts"]}\n')
        self.write("vitest.config.ts", "export default {};\n")
        self.write("tests/ya.make", f"TS_TEST_VITEST_FOR({PROJECT})\nTS_TEST_SRCS(../src)\nTS_TEST_CONFIG(../vitest.config.ts)\nEND()\n")
        self.write("src/answer.spec.ts", source)

    def test_typescript_import_of_a_missing_module(self):
        self.ts_imports('import "./absent";\n')
        self.assertConfigureLikeYa()

    def test_typescript_import_without_a_string(self):
        self.ts_imports("import value from answer;\n")
        self.assertConfigureLikeYa()

    def test_typescript_quote_after_a_failed_import(self):
        self.ts_imports("let stdout = '';\nlet stderr = '';\nconst kinds = ['import'];\nconst code = 'import(no-cycle)';\n")
        self.assertConfigureLikeYa()

    def test_partitioned_recurse_of_a_missing_directory(self):
        self.write("ya.make", "PARTITIONED_RECURSE(missing)\n")
        self.assertConfigureLikeYa()

    def partitioned(self, config, path=f"{PROJECT}/balancing.json"):
        self.write("ya.make", f"SET(RECURSE_PARTITIONS_COUNT 2)\nSET(RECURSE_PARTITION_INDEX 0)\nPARTITIONED_RECURSE(one two BALANCING_CONFIG {path})\n")
        for sub in ("one", "two"):
            self.write(f"{sub}/ya.make", "LIBRARY()\nSRCS(value.cpp)\nEND()\n")
            self.write(f"{sub}/value.cpp", "int value() { return 42; }\n")
        if config is not None:
            self.write("balancing.json", config)

    def test_partitioned_recurse_with_a_missing_balancing_config(self):
        self.partitioned(None, path="balancing.json")
        self.assertConfigureLikeYa()

    def test_partitioned_recurse_with_an_invalid_balancing_config(self):
        self.partitioned("{broken\n")
        self.assertConfigureLikeYa()

    def test_partitioned_recurse_without_the_partition_count(self):
        self.partitioned(json.dumps({"partitions": {"3": {"0": ["one"]}}}))
        self.assertConfigureLikeYa()

    def ts_tests(self, *statements):
        self.write("lib/ya.make", "TS_LIBRARY()\nTS_BUILD_SCRIPT(nots:build)\nTS_BUILD_OUTPUTS(dist)\n" + "".join(s + "\n" for s in statements) + "END()\n")
        self.write("lib/package.json", json.dumps({"name": "@e2e/lib", "version": "1.0.0", "engines": {"node": nodejs_version()}, "files": ["src", "dist"], "scripts": {"nots:build": "tsc"}}))
        self.write("lib/pnpm-lock.yaml", "lockfileVersion: '9.0'\nsettings:\n  autoInstallPeers: true\n  excludeLinksFromLockfile: false\nimporters:\n  .: {}\n")
        self.write("lib/tsconfig.json", json.dumps({"compilerOptions": {"rootDir": "src", "outDir": "dist"}, "include": ["src/**/*.ts"]}))
        self.write("lib/src/answer.ts", "export const answer = 42;\n")
        self.write("lib/src/answer.css", "a { color: red; }\n")
        self.write("ya.make", "RECURSE(lib)\n")

    def test_stylelint_with_a_missing_config(self):
        self.ts_tests("TS_STYLELINT(missing.json)")
        self.assertConfigureLikeYa()

    def test_biome_with_a_missing_config(self):
        self.ts_tests("TS_BIOME(missing.json)")
        self.assertConfigureLikeYa()


class ContribVersionConfigureErrorsTest(MakeTest):
    """Explicit VERSION for modules in contrib (devtools/ymake/general_parser.cpp)."""

    project_name = "contrib/__ay_e2e__"

    def test_contrib_library_without_a_version(self):
        self.write("ya.make", "LIBRARY()\nLICENSE(MIT)\nSRCS(value.cpp)\nEND()\n")
        self.write("value.cpp", "int value() { return 42; }\n")
        self.assertConfigureLikeYa()
