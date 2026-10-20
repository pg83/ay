import unittest

import lib


BARE = "NO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"


class TestCodegenModules(unittest.TestCase):
    def test_usrv_all_sources_filters_recursive_globs(self):
        graph = lib.make({
            "a/ya.make": "LIBRARY()\n" + BARE + "USRV_ALL_SRCS(RECURSIVE src include EXCLUDE **/*_test.cpp)\nEND()\n",
            "a/src/a.cpp": "", "a/src/nested/b.cc": "", "a/src/a_test.cpp": "",
            "a/include/a.hpp": "", "a/src/data.json": "{}",
        }, "a")
        outputs = {o for n in graph["graph"] for o in n["outputs"]}
        self.assertIn("$(B)/a/_/src/a.cpp.o", outputs)
        self.assertIn("$(B)/a/_/src/nested/b.cc.o", outputs)
        self.assertNotIn("$(B)/a/_/src/a_test.cpp.o", outputs)

    def test_explicit_style_checks_use_configs_and_selected_files(self):
        files = {
            "a/ya.make": "LIBRARY()\n" + BARE + "SRCS(a.cpp)\nSTYLE_JSON()\nSTYLE_YAML(DIRS_RECURSE configs)\nEND()\n",
            "a/a.cpp": "", "a/a.json": "{}", "a/a.yaml": "name: ignored", "a/service.yaml": "name: test",
            "a/configs/sub/options.yml": "option: true", "a/canondata/result.json": "{}",
            "build/config/tests/json_style/default_configs.json": '{"clang_format_json": "config/json.conf"}',
            "build/config/tests/yaml_style/default_configs.json": '{"yamlfmt": "config/yaml.conf"}',
            "config/json.conf": "", "config/yaml.conf": "",
            "tools/clang_format_json_linter/wrapper.py": "", "tools/yamlfmt_linter/wrapper.py": "",
            "build/platform/clang/clang-format/ya.make": "RESOURCES_LIBRARY()\nDECLARE_EXTERNAL_RESOURCE(CLANG_FORMAT sbr:42)\nEND()\n",
            "build/external_resources/yamlfmt/ya.make": "RESOURCES_LIBRARY()\nDECLARE_EXTERNAL_RESOURCE(YAMLFMT sbr:43)\nEND()\n",
        }
        graph = lib.make(files, "a", "-t")
        for name, config, selected in [("clang_format_json", "json", ["a.json"]), ("yamlfmt", "yaml", ["service.yaml", "configs/sub/options.yml"])]:
            node = lib.node_by_output(graph, f"$(B)/a/test-results/{name}/meta.json")
            args = node["cmds"][0]["cmd_args"]
            self.assertIn("run_custom_lint", args)
            self.assertIn(f"$(S)/config/{config}.conf", args)
            self.assertIn("style", args)
            for file in selected:
                self.assertIn(f"$(S)/a/{file}", node["inputs"])
            self.assertNotIn("$(S)/a/a.yaml", node["inputs"])
            self.assertNotIn("$(S)/a/canondata/result.json", node["inputs"])

        files["a/ya.make"] = files["a/ya.make"].replace("STYLE_JSON()", "USRV_STYLE_JSON_CURRENT_DIRECTORY()").replace("STYLE_YAML(DIRS_RECURSE configs)", "USRV_STYLE_YAML_CURRENT_DIRECTORY()")
        graph = lib.make(files, "a", "-t")
        for name in ("clang_format_json", "yamlfmt"):
            node = lib.node_by_output(graph, f"$(B)/a/test-results/{name}/meta.json")
            self.assertNotIn("$(S)/a/configs/sub/options.yml", node["inputs"])

    def test_yaml_style_with_config_needs_collected_sources(self):
        # STYLE_YAML_WITH_CONFIG lints $YAML_LINT_SRCS (set only by
        # COLLECT_YAML_LINT_SRCS); _ADD_CUSTOM_EXPLICIT_LINTER_CHECK peers the
        # yamlfmt resource regardless (build/plugins/ytest.py).
        files = {
            "a/ya.make": "LIBRARY()\n" + BARE + "SRCS(a.cpp)\nEND()\n",
            "a/a.cpp": "", "a/service.yaml": "name: test",
            "build/conf/autoincludes.json": '["a"]',
            "a/linters.make.inc": "STYLE_YAML_WITH_CONFIG(CONFIG_TYPE .yamlfmt.yml)\n",
            "build/external_resources/yamlfmt/ya.make": "RESOURCES_LIBRARY()\nDECLARE_EXTERNAL_RESOURCE(YAMLFMT sbr:43)\nEND()\n",
        }
        graph = lib.make(files, "a", "-t")
        self.assertFalse(any("/test-results/yamlfmt/" in o for n in graph["graph"] for o in n["outputs"]))
        del files["build/external_resources/yamlfmt/ya.make"]
        result = lib.make_process(files, "a", "-t")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("build/external_resources/yamlfmt", result.stderr)

    def test_generated_library_can_be_both_peer_and_recursed_root(self):
        files = {
            "a/ya.make": "LIBRARY()\n" + BARE + "SRCS(a.cpp)\nPEERDIR(gen)\nEND()\nRECURSE_FOR_TESTS(ut)\n",
            "a/a.cpp": '#include "gen/generated.h"\n',
            "a/ut/ya.make": "UNITTEST()\n" + BARE + "SRCS(test.cpp)\nPEERDIR(a)\nEND()\n",
            "a/ut/test.cpp": "",
            "library/cpp/testing/unittest_main/ya.make": "LIBRARY()\n" + BARE + "END()\n",
            "gen/ya.make": "GEN_LIBRARY()\nRUN_PROGRAM(tools/gen OUT_NOAUTO generated.h)\nEND()\n",
        }
        lib.tool_program(files, "tools/gen", "gen")
        graph = lib.make(files, "a", "-t")
        self.assertEqual(sum("$(B)/gen/generated.h" in n["outputs"] for n in graph["graph"]), 1)
        self.assertIsNotNone(lib.node_by_output(graph, "$(B)/a/ut/test-results/unittest/meta.json"))

    def test_no_build_if_discards_test_module(self):
        files = {"a/ya.make": "EXECTEST()\nNO_BUILD_IF(STRICT SANDBOXING)\nRUN(no-such-program)\nDEPENDS(no/such/module)\nEND()\n"}
        graph = lib.make(files, "a", "-t")
        self.assertFalse(any("/a/" in o for n in graph["graph"] for o in n["outputs"]))

    def test_gen_library_keeps_producers_without_link_archive(self):
        files = {
            "gen/ya.make": "GEN_LIBRARY()\nRUN_PROGRAM(tools/gen OUT_NOAUTO generated.h)\nEND()\n",
            "a/ya.make": "LIBRARY()\n" + BARE + "PEERDIR(gen)\nSRCS(a.cpp)\nEND()\n",
            "a/a.cpp": '#include "gen/generated.h"\n',
        }
        lib.tool_program(files, "tools/gen", "gen")
        graph = lib.make(files, "a")
        generated = lib.node_by_output(graph, "$(B)/gen/generated.h")
        cc = lib.node_by_output(graph, "$(B)/a/a.cpp.o")
        self.assertIn(generated["uid"], cc["deps"])
        self.assertFalse(any(o.endswith(".a") and o.startswith("$(B)/gen/") for n in graph["graph"] for o in n["outputs"]))

    def test_usrv_build_uses_registered_generated_headers_and_declared_includes(self):
        files = {
            "a/ya.make": "LIBRARY()\n" + BARE +
                "RUN_PROGRAM(tools/gen OUT_NOAUTO generated.tar)\n"
                "USRV_BUILD(FROM ${BINDIR}/generated.tar DEPS_FILE deps.json generated.h generated.cpp)\nEND()\n",
            "a/deps.json": '{"generated.h": ["dep.h"], "generated.cpp": ["generated.h"]}',
            "a/dep.h": "#pragma once\n",
            "taxi/uservices/arc-gen/unpack-tar/usrv-build-lib/ya.make": "RESOURCES_LIBRARY()\nDECLARE_EXTERNAL_RESOURCE(USRV_UNPACK_TAR sbr:1)\nEND()\n",
        }
        lib.tool_program(files, "tools/gen", "gen")
        graph = lib.make(files, "a")
        header = lib.node_by_output(graph, "$(B)/a/generated.h")
        self.assertEqual(header["kv"]["p"], "UT")
        args = header["cmds"][0]["cmd_args"]
        self.assertIn("--source", args)
        cc = lib.node_by_output(graph, "$(B)/a/generated.cpp.o")
        self.assertIn(header["uid"], cc["deps"])
        self.assertIn("$(S)/a/dep.h", cc["inputs"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
