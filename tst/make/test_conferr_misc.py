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

    def test_go_test_for_its_own_directory(self):
        self.write("ya.make", f"GO_TEST_FOR({PROJECT})\nGO_TEST_SRCS(value_test.go)\nEND()\n")
        self.write("value_test.go", "package value\n")
        self.assertConfigureLikeYa()

    def test_dll_for_its_own_directory(self):
        self.write("ya.make", f"DLL_FOR({PROJECT} answer)\nEND()\n")
        self.assertConfigureLikeYa()

    def test_two_commands_with_the_same_output(self):
        self.library("RUN_PYTHON3(first.py STDOUT out.txt)", "RUN_PYTHON3(second.py STDOUT out.txt)")
        self.write("first.py", "print(1)\n")
        self.write("second.py", "print(2)\n")
        self.assertConfigureLikeYa()

    def test_command_without_outputs(self):
        self.library("RUN_PYTHON3(gen.py)")
        self.write("gen.py", "print(42)\n")
        self.assertConfigureLikeYa()

    def test_output_named_as_the_module(self):
        self.write("ya.make", "PROGRAM(answer)\nSRCS(main.cpp)\nRUN_PYTHON3(gen.py STDOUT answer)\nEND()\n")
        self.write("main.cpp", "int main() { return 0; }\n")
        self.write("gen.py", "print(42)\n")
        self.assertConfigureLikeYa()

    def test_include_of_an_undefined_macro(self):
        self.library()
        self.write("value.cpp", "#include UNDEFINED_HEADER\nint value() { return 42; }\n")
        self.assertConfigureLikeYa()

    def sysincl(self, text):
        core = self.root / "build/ymake.core.conf"
        self.write_tree("build/ymake.core.conf", core.read_text() + f"\nSYSINCL+={PROJECT}/sysincl.yml\n")
        self.write("sysincl.yml", text)

    def test_sysincl_to_a_missing_target(self):
        self.sysincl(f"- includes:\n  - e2e_value.h: {PROJECT}/missing/e2e_value.h\n")
        self.library()
        self.write("value.cpp", "#include <e2e_value.h>\nint value() { return 42; }\n")
        self.assertConfigureLikeYa()

    def test_sysincl_and_addincl_mismatch(self):
        self.sysincl(f"- includes:\n  - e2e_value.h: {PROJECT}/sys/e2e_value.h\n")
        self.library(f"ADDINCL({PROJECT}/inc)")
        self.write("value.cpp", '#include "e2e_value.h"\nint value() { return 42; }\n')
        self.write("sys/e2e_value.h", "#pragma once\n")
        self.write("inc/e2e_value.h", "#pragma once\n")
        self.assertConfigureLikeYa()

    def test_recurse_into_an_unreadable_ya_make(self):
        self.write("ya.make", "RECURSE(sub)\n")
        self.write("sub/ya.make", "LIBRARY()\nSRCS(value.cpp)\nEND()\n")
        self.write("sub/value.cpp", "int value() { return 42; }\n")
        (self.project / "sub/ya.make").chmod(0)
        self.addCleanup((self.project / "sub/ya.make").chmod, 0o644)
        self.assertConfigureLikeYa()

    def conf(self, text):
        core = self.root / "build/ymake.core.conf"
        self.write_tree("build/ymake.core.conf", core.read_text() + "\n" + text + "\n")

    def test_custom_blacklist_that_is_missing(self):
        self.conf(f"CUSTOM_BLACKLIST={PROJECT}/missing.blacklist")
        self.library()
        self.assertConfigureLikeYa()

    def test_custom_isolated_projects_that_are_missing(self):
        self.conf(f"CUSTOM_ISOLATED_PROJECTS={PROJECT}/missing.list")
        self.library()
        self.assertConfigureLikeYa()

    def test_peer_policy_line_with_a_syntax_error(self):
        path = "build/rules/peerdir_policies/junk.policy"
        self.write_tree(path, (self.root / path).read_text() + "ALLOW a -> b c\n")
        self.library()
        self.assertConfigureLikeYa()

    def java_program(self):
        self.write("ya.make", "JAVA_PROGRAM(answer)\nJDK_VERSION(17)\nJAVA_SRCS(Main.java)\nEND()\n")
        self.write("Main.java", "package e2e; public class Main { public static void main(String[] args) {} }\n")

    def test_forced_dependency_management_exceptions_without_for(self):
        self.conf("_FORCED_DEPENDENCY_MANAGEMENT_EXCEPTIONS=e2e")
        self.java_program()
        self.assertConfigureLikeYa()

    def test_forced_dependency_management_exceptions_without_a_library(self):
        self.conf("_FORCED_DEPENDENCY_MANAGEMENT_EXCEPTIONS=FOR FOR contrib/java/e2e")
        self.java_program()
        self.assertConfigureLikeYa()

    def test_forced_dependency_management_exceptions_without_excludes(self):
        self.conf("_FORCED_DEPENDENCY_MANAGEMENT_EXCEPTIONS=FOR contrib/java/e2e FOR contrib/java/other e2e")
        self.java_program()
        self.assertConfigureLikeYa()

    def test_multimodule_with_a_sub_module_loop(self):
        self.conf("multimodule E2E_LOOP {\n    module E2E_FIRST: LIBRARY {\n        .PEERDIRSELF=E2E_SECOND\n    }\n    module E2E_SECOND: LIBRARY {\n        .PEERDIRSELF=E2E_FIRST\n    }\n}")
        self.library()
        self.assertConfigureLikeYa()

    def test_pytest_bin_with_an_unknown_argument(self):
        self.write("ya.make", "PY3TEST()\nTEST_SRCS(test_value.py)\nSETUP_PYTEST_BIN(e2e)\nEND()\n")
        self.write("test_value.py", "def test_value():\n    assert True\n")
        self.assertConfigureLikeYa()

    def test_sysincl_source_filter_that_is_not_a_regex(self):
        self.sysincl("- source_filter: \"[\"\n  includes:\n  - e2e_value.h\n")
        self.library()
        self.assertConfigureLikeYa()

    def test_source_in_an_unreadable_directory(self):
        self.library("SRCS(sub/value.cpp)")
        self.write("sub/value.cpp", "int sub() { return 42; }\n")
        (self.project / "sub").chmod(0)
        self.addCleanup((self.project / "sub").chmod, 0o755)
        self.assertConfigureLikeYa()

    def test_macros_with_error(self):
        self.library("MACROS_WITH_ERROR()")
        self.assertConfigureLikeYa()

    def test_java_classpath_cmd_type_that_is_unknown(self):
        self.write("ya.make", "JUNIT5()\nJDK_VERSION(17)\nJAVA_SRCS(AnswerTest.java)\nTEST_JAVA_CLASSPATH_CMD_TYPE(BOGUS)\nEND()\n")
        self.write("AnswerTest.java", "package e2e; public class AnswerTest {}\n")
        self.assertConfigureLikeYa()

    def test_style_cpp_with_an_unknown_config_type(self):
        self.library("STYLE_CPP(bogus)")
        self.assertConfigureLikeYa()

    def test_glob_assert_without_ya_make(self):
        self.conf("macro E2E_NO_MODULES_INSIDE() {\n    _GLOB(E2E_YAMAKES **/ya.make EXCLUDE ya.make)\n    _ASSERT_NO_YAMAKE($E2E_YAMAKES MSG no modules expected inside)\n}")
        self.library("E2E_NO_MODULES_INSIDE()")
        self.write("sub/ya.make", "LIBRARY()\nEND()\n")
        self.assertConfigureLikeYa()

    def test_module_scope_variable_outside_of_a_module(self):
        self.conf("_MODULE_SCOPE_ONLY_VARS=E2E_SCOPED\nE2E_SCOPED=value")
        self.write("ya.make", "LIBRARY()\nSRCS(value.cpp)\nEND()\nMESSAGE(${E2E_SCOPED})\n")
        self.write("value.cpp", "int value() { return 42; }\n")
        self.assertConfigureLikeYa()

    def test_too_many_license_properties(self):
        self.conf(f"LICENSES+={PROJECT}/license.json")
        self.write("license.json", json.dumps({f"E2E_GROUP_{i}": {"static": ["E2E-" + str(i)], "dynamic": ["E2E-" + str(i)]} for i in range(25)}))
        self.library()
        self.assertConfigureLikeYa()

    def test_test_dependency_not_listed_as_depends(self):
        self.conf("CHECK_DEPENDS_IN_DART=yes")
        self.unittest(f"SET_APPEND(TEST_DEPENDS_VALUE {PROJECT}/sub)")
        self.write("sub/ya.make", "PROGRAM()\nSRCS(main.cpp)\nEND()\n")
        self.write("sub/main.cpp", "int main() { return 0; }\n")
        self.assertConfigureLikeYa()

    def glob_macro(self, call):
        self.conf("macro E2E_GLOB() {\n    " + call + "\n}")
        self.library("E2E_GLOB()")

    def test_glob_restriction_without_a_value(self):
        self.glob_macro("_GLOB(E2E_FILES *.cpp RESTRICTIONS MAX_MATCHES)")
        self.assertConfigureLikeYa()

    def test_glob_restriction_with_an_invalid_value(self):
        self.glob_macro("_GLOB(E2E_FILES *.cpp RESTRICTIONS MAX_MATCHES many)")
        self.assertConfigureLikeYa()

    def test_glob_with_an_unknown_restriction(self):
        self.glob_macro("_GLOB(E2E_FILES *.cpp RESTRICTIONS E2E_LIMIT)")
        self.assertConfigureLikeYa()

    def test_glob_with_an_invalid_pattern(self):
        self.glob_macro("_GLOB(E2E_FILES e2e**.cpp)")
        self.assertConfigureLikeYa()

    def test_glob_with_two_recursive_parts(self):
        self.glob_macro("_GLOB(E2E_FILES **/sub/**/*.cpp)")
        self.assertConfigureLikeYa()

    def test_glob_with_too_many_matches(self):
        self.glob_macro("_GLOB(E2E_FILES *.cpp RESTRICTIONS MAX_MATCHES 1)")
        self.write("other.cpp", "int other() { return 42; }\n")
        self.assertConfigureLikeYa()

    def test_glob_with_too_many_watched_dirs(self):
        self.glob_macro("_GLOB(E2E_FILES **/*.cpp RESTRICTIONS MAX_WATCH_DIRS 1)")
        self.write("sub/other.cpp", "int other() { return 42; }\n")
        self.assertConfigureLikeYa()

    def test_late_glob_without_arguments(self):
        self.glob_macro("_LATE_GLOB()")
        self.assertConfigureLikeYa()

    def test_late_glob_with_an_invalid_pattern(self):
        self.glob_macro("_LATE_GLOB(E2E_FILES e2e**.cpp)")
        self.assertConfigureLikeYa()

    def test_program_with_need_add_fake_src(self):
        self.write("ya.make", "PROGRAM()\nENABLE(NEED_ADD_FAKE_SRC)\nEND()\n")
        self.assertConfigureLikeYa()

    def test_copy_file_with_one_argument(self):
        self.library("COPY_FILE(value.cpp)")
        self.assertConfigureLikeYa()

    def test_configure_file_with_one_argument(self):
        self.library("CONFIGURE_FILE(value.h.in)")
        self.write("value.h.in", "#pragma once\n")
        self.assertConfigureLikeYa()

    def output_includes(self, out):
        self.library(f"RUN_PROGRAM({PROJECT}/tool STDOUT {out} OUTPUT_INCLUDES value.h)")
        self.write("tool/ya.make", "PROGRAM()\nSRCS(main.cpp)\nEND()\n")
        self.write("tool/main.cpp", "int main() { return 0; }\n")
        self.write("value.h", "#pragma once\n")

    def test_output_includes_of_a_go_output(self):
        self.output_includes("out.go")
        self.assertConfigureLikeYa()

    def test_output_includes_of_a_flatbuffers_output(self):
        self.output_includes("out.fbs")
        self.assertConfigureLikeYa()

    def test_swig_include_with_empty_quotes(self):
        self.write("ya.make", "LIBRARY(named)\nSRCS(answer.swg)\nEND()\n")
        self.write("answer.swg", '%module answer\n%include ""\n')
        self.assertConfigureLikeYa()

    def ros_topic(self, text):
        self.library(f"RUN_PROGRAM({PROJECT}/tool IN value.rostopic STDOUT out.cpp)")
        self.write("tool/ya.make", "PROGRAM()\nSRCS(main.cpp)\nEND()\n")
        self.write("tool/main.cpp", "int main() { return 0; }\n")
        self.write("value.rostopic", text)

    def test_ros_topic_type_without_a_package(self):
        self.ros_topic("type: Answer\n")
        self.assertConfigureLikeYa()

    def test_ros_topic_without_a_type(self):
        self.ros_topic("name: answer\n")
        self.assertConfigureLikeYa()

    def test_induced_deps_with_a_relative_path(self):
        self.write("ya.make", "PROGRAM()\nSRCS(main.cpp)\nINDUCED_DEPS(h value.h)\nEND()\n")
        self.write("main.cpp", "int main() { return 0; }\n")
        self.write("value.h", "#pragma once\n")
        self.assertConfigureLikeYa()

    def test_cython_coverage_include_of_a_missing_file(self):
        self.write("ya.make", "PY3_LIBRARY()\nSET(CYTHON_COVERAGE yes)\nPY_SRCS(value.pyx)\nEND()\n")
        self.write("value.pyx", 'include "missing.pxi"\n')
        self.assertConfigureLikeYa()

    def test_stdin_resource_with_an_empty_value(self):
        self.library("RESOURCE(- conftest.py=)")
        self.assertConfigureLikeYa()

    def test_conf_macro_command_without_a_kv_p(self):
        self.conf("macro E2E_GEN(Out) {\n    .CMD=$YMAKE_PYTHON3 -c pass ${output:Out}\n}")
        self.library("E2E_GEN(out.cpp)")
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
