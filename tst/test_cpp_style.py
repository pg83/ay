import unittest
import lib

class CppStyleTest(unittest.TestCase):
    def test_custom_cpp_linters_use_declared_formatter_dependencies(self):
        for macro, name, tool, extras in (
            ("STYLE_CPP_USER_SESSIONS", "clang_format_18_user_sessions", "quality/user_sessions/tools/clang-format", [
                "custom_clang_format=quality/user_sessions/tools/clang-format", "custom_clang_format_bin=clang-format",
            ]),
            ("STYLE_CPP_YT", "clang_format_yt", "ads/core/contrib/libs/clang16/tools/clang-format", [
                "custom_clang_format=ads/core/contrib/libs/clang16/tools/clang-format",
            ]),
        ):
            with self.subTest(macro=macro):
                files = {
                    "build/conf/autoincludes.json": '["quality/user_sessions/t21test"]\n',
                    "quality/user_sessions/t21test/linters.make.inc": macro + "(CONFIG_TYPE .clang-format)\n",
                    "quality/user_sessions/t21test/.clang-format": "BasedOnStyle: LLVM\n",
                    "quality/user_sessions/t21test/lib/ya.make": "LIBRARY()\nNO_UTIL()\nNO_LIBC()\nNO_RUNTIME()\nSRCS(a.cpp)\nEND()\n",
                    "quality/user_sessions/t21test/lib/a.cpp": "int a;\n",
                    "quality/user_sessions/t21test/lib/a.h": "int a;\n",
                }
                lib.tool_program(files, tool, "clang-format")
                graph = lib.make(files, "quality/user_sessions/t21test/lib", "-t")
                run = lib.node_by_output(graph, "$(B)/quality/user_sessions/t21test/lib/test-results/" + name + "/meta.json")
                formatter = lib.node_by_output(graph, "$(B)/" + tool + "/clang-format")
                context = lib.node_by_output(graph, "$(B)/common_test.context")
                self.assertCountEqual(run["deps"], [formatter["uid"], context["uid"]])
                self.assertNotIn("$(B)/" + tool + "/clang-format", run["inputs"])
                args = run["cmds"][0]["cmd_args"]
                depends = [args[i + 1] for i, arg in enumerate(args) if arg == "--depends"]
                self.assertEqual(depends, sorted(["quality/user_sessions/t21test/lib", tool + "/clang-format"]))
                self.assertEqual([args[i + 1] for i, arg in enumerate(args) if arg == "--extra-param"], extras)
                self.assertEqual(args[-2:], ["$(S)/quality/user_sessions/t21test/lib/a.cpp", "$(S)/quality/user_sessions/t21test/lib/a.h"])
                self.assertIn("$(S)/quality/user_sessions/t21test/.clang-format", args)

    def test_standard_cpp_style_collects_resolved_sources_and_headers(self):
        files = {
            "build/conf/autoincludes.json": '["proj"]\n',
            "proj/linters.make.inc": "STYLE_CPP(CONFIG_TYPE .clang-format)\n",
            "proj/.clang-format": "BasedOnStyle: LLVM\n",
            "proj/lib/ya.make": "LIBRARY()\nNO_UTIL()\nNO_LIBC()\nNO_RUNTIME()\nSRCDIR(other)\nSRCS(a.cpp)\nCOPY_FILE(AUTO code.txt generated.cpp)\nEND()\n",
            "other/a.cpp": "int a;\n",
            "proj/lib/a.h": "int a;\n",
            "proj/lib/a.inc": "",
            "proj/lib/nested/nested.h": "",
            "proj/lib/code.txt": "int generated;\n",
            "build/platform/clang/clang-format/ya.make": "RESOURCES_LIBRARY()\nDECLARE_EXTERNAL_RESOURCE(CLANG_FORMAT sbr:123)\nEND()\n",
        }
        graph = lib.make(files, "proj/lib", "-t")
        run = lib.node_by_output(graph, "$(B)/proj/lib/test-results/clang_format/meta.json")
        self.assertEqual(run["cmds"][0]["cmd_args"][-2:], ["$(S)/other/a.cpp", "$(S)/proj/lib/a.h"])
        command = run["cmds"][0]["cmd_args"]
        self.assertEqual([command[i + 1] for i, arg in enumerate(command) if arg == "--global-resource"], [
            "CLANG_FORMAT_RESOURCE_GLOBAL::$(B)/resources/CLANG_FORMAT",
        ] * 2)
        self.assertEqual([command[i + 1] for i, arg in enumerate(command) if arg == "--config"], ["$(S)/proj/.clang-format"])
        context = lib.node_by_output(graph, "$(B)/common_test.context")
        self.assertEqual(run["deps"], [context["uid"]])
        self.assertFalse(any("generated.cpp" in i for i in run["inputs"]))

    def test_cpp_style_includes_native_link_command_sources(self):
        files = {"build/platform/clang/clang-format/ya.make": "RESOURCES_LIBRARY()\nEND()\n"}
        lib.tool_program(files, "prog", "app")
        files["prog/ya.make"] = files["prog/ya.make"].replace("END()", "STYLE_CPP()\nEND()")
        graph = lib.make(files, "prog", "-t")
        run = lib.node_by_output(graph, "$(B)/prog/test-results/clang_format/meta.json")
        self.assertIn("$(S)/build/scripts/c_templates/svn_interface.c", run["inputs"])
        self.assertIn("$(S)/build/scripts/c_templates/svn_interface.c", run["cmds"][0]["cmd_args"])

    def test_cpp_style_emits_a_suite_without_cpp_sources(self):
        files = {
            "lib/ya.make": "LIBRARY()\nNO_UTIL()\nNO_LIBC()\nNO_RUNTIME()\nSTYLE_CPP()\nEND()\n",
            "build/platform/clang/clang-format/ya.make": "RESOURCES_LIBRARY()\nEND()\n",
        }
        graph = lib.make(files, "lib", "-t")
        run = lib.node_by_output(graph, "$(B)/lib/test-results/clang_format/meta.json")
        command = run["cmds"][0]["cmd_args"]
        self.assertEqual(command[-2:], ["--config", "$(S)/build/config/tests/cpp_style/.clang-format"])
        self.assertEqual(run["deps"], [lib.node_by_output(graph, "$(B)/common_test.context")["uid"]])

    def test_cpp_style_reads_the_declared_default_config_map(self):
        files = {
            "lib/ya.make": "LIBRARY()\nNO_UTIL()\nNO_LIBC()\nNO_RUNTIME()\nSET(CPP_LINTERS_DEFAULT_CONFIGS configs/cpp.json)\nSTYLE_CPP()\nEND()\n",
            "configs/cpp.json": '{"clang_format": "configs/.clang-format"}',
            "configs/.clang-format": "BasedOnStyle: LLVM\n",
            "build/platform/clang/clang-format/ya.make": "RESOURCES_LIBRARY()\nEND()\n",
        }
        graph = lib.make(files, "lib", "-t")
        run = lib.node_by_output(graph, "$(B)/lib/test-results/clang_format/meta.json")
        self.assertIn("$(S)/configs/.clang-format", run["inputs"])
        self.assertNotIn("$(S)/build/config/tests/cpp_style/.clang-format", run["inputs"])

    def test_cpp_style_validates_autoinclude_configs(self):
        for config_type, error in (
            ("unknown", "unknown clang_format config type"),
            (".clang-format", "missing config proj/.clang-format"),
        ):
            with self.subTest(config_type=config_type):
                files = {
                    "build/conf/autoincludes.json": '["proj"]\n',
                    "proj/linters.make.inc": "STYLE_CPP(CONFIG_TYPE " + config_type + ")\n",
                    "proj/lib/ya.make": "LIBRARY()\nNO_UTIL()\nNO_LIBC()\nNO_RUNTIME()\nEND()\n",
                    "build/platform/clang/clang-format/ya.make": "RESOURCES_LIBRARY()\nEND()\n",
                }
                result = lib.make_process(files, "proj/lib", "-t")
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(error, result.stderr)

    def test_cpp_style_obeys_no_lint_after_the_macro(self):
        files = {
            "lib/ya.make": "LIBRARY()\nNO_UTIL()\nNO_LIBC()\nNO_RUNTIME()\nSTYLE_CPP()\nNO_LINT()\nSRCS(a.cpp)\nEND()\n",
            "lib/a.cpp": "int a;\n",
            "build/platform/clang/clang-format/ya.make": "RESOURCES_LIBRARY()\nEND()\n",
        }
        graph = lib.make(files, "lib", "-t")
        self.assertFalse(any(n["kv"].get("run_test_node") for n in graph["graph"]))

    def test_custom_cpp_formatter_is_restricted_to_upstream_prefixes(self):
        files = {
            "lib/ya.make": "LIBRARY()\nNO_UTIL()\nNO_LIBC()\nNO_RUNTIME()\nSTYLE_CPP_USER_SESSIONS()\nSRCS(a.cpp)\nEND()\n",
            "lib/a.cpp": "int a;\n",
        }
        result = lib.make_process(files, "lib", "-t")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("not allowed", result.stderr)

    def test_native_unittest_without_cpp_style_has_no_cpp_linter(self):
        from test_link_test_nodes import fixture
        graph = lib.make(fixture(), "lib/ut", "-t")
        self.assertFalse(any("$(B)/lib/ut/test-results/clang_format/meta.json" in n["outputs"] for n in graph["graph"]))

    def test_repeated_cpp_style_declarations_share_one_suite(self):
        files = {
            "build/conf/autoincludes.json": '["lib"]\n',
            "lib/linters.make.inc": "STYLE_CPP()\n",
            "lib/ya.make": "LIBRARY()\nNO_UTIL()\nNO_LIBC()\nNO_RUNTIME()\nSTYLE_CPP()\nSRCS(a.cpp)\nEND()\n",
            "lib/a.cpp": "int a;\n",
            "build/platform/clang/clang-format/ya.make": "RESOURCES_LIBRARY()\nEND()\n",
        }
        graph = lib.make(files, "lib", "-t")
        checks = [n for n in graph["graph"] if "$(B)/lib/test-results/clang_format/meta.json" in n["outputs"]]
        self.assertEqual(len(checks), 1)
        self.assertEqual(checks[0]["cmds"][0]["cmd_args"].count("$(S)/lib/a.cpp"), 2)

    def test_explicit_cpp_style_adds_one_suite_to_native_unittest(self):
        from test_link_test_nodes import fixture
        files = fixture()
        files["lib/ut/ya.make"] = files["lib/ut/ya.make"].replace("END()", "STYLE_CPP()\nEND()")
        files["build/platform/clang/clang-format/ya.make"] = "RESOURCES_LIBRARY()\nEND()\n"
        files["build/config/tests/cpp_style/default_configs.json"] = '{"clang_format": "build/config/tests/cpp_style/.clang-format"}'
        graph = lib.make(files, "lib/ut", "-t")
        checks = [n for n in graph["graph"] if "$(B)/lib/ut/test-results/clang_format/meta.json" in n["outputs"]]
        self.assertEqual(len(checks), 1)
        self.assertIn("$(S)/build/scripts/c_templates/svn_interface.c", checks[0]["inputs"])
        self.assertIn("$(S)/lib/h.h", checks[0]["inputs"])

if __name__ == "__main__":
    unittest.main(verbosity=2)
