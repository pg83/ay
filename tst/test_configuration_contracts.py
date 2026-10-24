import unittest

import lib
from test_python_suites import fixture as python_fixture
from test_ymake_conf import Tree, cc_args, library


class ConfigurationContractsTest(unittest.TestCase):
    def test_size_optimization_uses_compiler_specific_flags(self):
        for release, size, clang, expected in (
            ("no", "no", "yes", None), ("yes", "no", "yes", "-O3"),
            ("yes", "yes", "yes", "-Oz"), ("yes", "yes", "no", "-Os"),
        ):
            with self.subTest(release=release, size=size, clang=clang):
                tree = Tree(self, {
                    "app/ya.make": library("SRCS(main.cpp)\n"), "app/main.cpp": "",
                    "build/ymake_conf.py": lib.ymake_conf(extra=f"_C_FLAGS=$OPTIMIZE\n_BUILD_RELEASE={release}\n_BUILD_SIZE_OPTIMIZED={size}\nCLANG={clang}\n"),
                })
                args = cc_args(tree.graph("app"), "$(B)/app/main.cpp.o")
                self.assertEqual([a for a in args if a in ("-O3", "-Oz", "-Os")], [expected] if expected else [])

    def test_unknown_compiler_flag_is_diagnosed(self):
        result = Tree(self, {
            "app/ya.make": library("SRCS(main.cpp)\n"), "app/main.cpp": "",
            "build/ymake_conf.py": lib.ymake_conf(extra="_C_FLAGS=$UNMODELLED_COMPILER_FLAG\n"),
        }).make("app")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("compiler flag $UNMODELLED_COMPILER_FLAG is not modelled", result.stderr)

    def test_rooted_dll_for_preserves_the_source_dependency(self):
        graph = lib.make({
            "shared/ya.make": "DLL_FOR(${ARCADIA_ROOT}/native named)\n",
            "native/ya.make": library("SRCS(source.cpp)\n"), "native/source.cpp": "",
        }, "shared")
        dll = lib.node_by_output(graph, "$(B)/shared/libnamed.so")
        compiled = next(n for n in graph["graph"] if "$(S)/native/source.cpp" in n["inputs"])
        self.assertIn(compiled["uid"], dll["deps"])

    def test_dll_for_rejects_missing_build_and_recursive_directories(self):
        for files, message in (
            ({"shared/ya.make": "DLL_FOR()\n"}, "expects a source directory"),
            ({"shared/ya.make": "DLL_FOR(${ARCADIA_BUILD_ROOT}/native)\n"}, "expects a source directory"),
            ({"shared/ya.make": "DLL_FOR(first)\n", "first/ya.make": "DLL_FOR(second)\n",
              "second/ya.make": "DLL_FOR(first)\n"}, "recursive module inclusion"),
        ):
            with self.subTest(message=message):
                result = lib.make_process(files, "shared")
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(message, result.stderr)
                self.assertNotIn("panic:", result.stderr)

    def test_lint_disabled_by_configuration_does_not_resolve_resources(self):
        for setting in ("NO_LINT()", "SET(_NO_LINT_VALUE none)",
                        "SET(_NO_LINT_VALUE none_internal)", "SET(TIDY yes)"):
            with self.subTest(setting=setting):
                files = {"app/ya.make": library(setting + "\nSTYLE_JSON()\nSTYLE_YAML()\nSTYLE_YAML_WITH_CONFIG()\n"),
                         "app/settings.json": "{}", "app/settings.yaml": "key: value"}
                graph = lib.make(files, "app", "-t")
                self.assertFalse(any(n["kv"].get("run_test_node") for n in graph["graph"]))

    def test_plain_no_lint_scalar_keeps_style_suites_and_real_resource_dependencies(self):
        for setting in ("SET(NO_LINT none)", "SET(NO_LINT none_internal)"):
            with self.subTest(setting=setting):
                files = {
                    "app/ya.make": library(setting + "\nSTYLE_JSON()\nSTYLE_YAML()\nSTYLE_YAML_WITH_CONFIG()\n"),
                    "app/settings.json": "{}", "app/settings.yaml": "key: value",
                    "build/config/tests/json_style/default_configs.json": '{"clang_format_json":"lint/json.conf"}',
                    "build/config/tests/yaml_style/default_configs.json": '{"yamlfmt":"lint/yaml.conf"}',
                    "lint/json.conf": "{}", "lint/yaml.conf": "{}",
                    "tools/clang_format_json_linter/wrapper.py": "",
                    "tools/yamlfmt_linter/wrapper.py": "",
                    "build/platform/clang/clang-format/ya.make": lib.resource_lib("CLANG_FORMAT", 21),
                    "build/external_resources/yamlfmt/ya.make": lib.resource_lib("YAMLFMT", 22),
                }
                graph = lib.make(files, "app", "-t")
                for suite, source, config, wrapper, resource, uri in (
                    ("clang_format_json", "settings.json", "json", "clang_format_json", "CLANG_FORMAT", "sbr:21"),
                    ("yamlfmt", "settings.yaml", "yaml", "yamlfmt", "YAMLFMT", "sbr:22"),
                ):
                    run = lib.node_by_output(graph, f"$(B)/app/test-results/{suite}/meta.json")
                    args = run["cmds"][0]["cmd_args"]
                    for path in (f"$(S)/app/{source}", f"$(S)/lint/{config}.conf",
                                 f"$(S)/tools/{wrapper}_linter/wrapper.py"):
                        self.assertIn(path, run["inputs"])
                        self.assertIn(path, args)
                    fetch = lib.node_by_output(graph, "$(B)/resources/" + resource)
                    self.assertIn(uri, fetch["cmds"][0]["cmd_args"])
                    self.assertIn(resource + "_RESOURCE_GLOBAL::" + fetch["outputs"][0], args)
                ids = {node["uid"] for node in graph["graph"]}
                self.assertTrue(graph["result"])
                self.assertTrue(set(graph["result"]) <= ids)
                self.assertTrue(all(set(node["deps"]) <= ids for node in graph["graph"]))

    def test_json_style_config_override_and_empty_directory(self):
        files = {
            "app/ya.make": library("SET(JSON_LINTER_DEFAULT_CONFIGS custom/maps.json)\nSTYLE_JSON(DIRS config empty)\n"),
            "app/config/settings.json": "{}", "app/empty/readme.txt": "",
            "custom/maps.json": '{"clang_format_json": "custom/style.conf"}', "custom/style.conf": "",
            "build/platform/clang/clang-format/ya.make": lib.resource_lib("CLANG_FORMAT", 21),
        }
        graph = lib.make(files, "app", "-t")
        run = lib.node_by_output(graph, "$(B)/app/test-results/clang_format_json/meta.json")
        self.assertIn("$(S)/custom/style.conf", run["cmds"][0]["cmd_args"])
        self.assertIn("$(S)/app/config/settings.json", run["inputs"])
        files["app/ya.make"] = library("STYLE_JSON(DIRS empty)\n")
        graph = lib.make(files, "app", "-t")
        self.assertFalse(any(n["kv"].get("run_test_node") for n in graph["graph"]))

    def test_linter_config_map_requires_a_json_object(self):
        for content, message in (("{broken", "invalid character"), ("null", "missing clang_format_json config"), ("[]", "expected JSON object")):
            with self.subTest(content=content):
                files = {
                    "app/ya.make": library("SET(JSON_LINTER_DEFAULT_CONFIGS custom/maps.json)\nSTYLE_JSON(DIRS config)\n"),
                    "app/config/settings.json": "{}", "custom/maps.json": content,
                    "build/platform/clang/clang-format/ya.make": lib.resource_lib("CLANG_FORMAT", 21),
                }
                result = lib.make_process(files, "app", "-t")
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(message, result.stderr)
                self.assertNotIn("panic:", result.stderr)

    def test_nested_modules_are_rejected_by_recursive_collectors(self):
        for macro in ("STYLE_JSON(DIRS_RECURSE .)", "USRV_ALL_SRCS(RECURSIVE)"):
            with self.subTest(macro=macro):
                result = lib.make_process({
                    "app/ya.make": library(macro + "\n"), "app/child/ya.make": library(""),
                }, "app", "-t")
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("nested module app/child/ya.make", result.stderr)

    def test_missing_linter_config_and_map_key_are_diagnosed(self):
        files = python_fixture("SET(MODULE_COMMON_CONFIGS_DIR absent)\nSTYLE_RUFF(CONFIG_TYPE ruff.toml)\n")
        result = lib.make_process(files, "lib", "-t")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("missing config absent/ruff.toml", result.stderr)
        files = python_fixture("STYLE_RUFF()\n")
        files["build/config/tests/py_style/default_configs.json"] = '{"flake8": "lint/flake8.conf"}'
        result = lib.make_process(files, "lib", "-t")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("missing ruff config", result.stderr)

    def test_usrv_default_sources_skip_own_makefile(self):
        graph = lib.make({"app/ya.make": library("USRV_ALL_SRCS()\n"), "app/a.cpp": "", "app/b.cc": ""}, "app")
        for output in ("$(B)/app/a.cpp.o", "$(B)/app/b.cc.o"):
            lib.node_by_output(graph, output)


if __name__ == "__main__":
    unittest.main(verbosity=2)
