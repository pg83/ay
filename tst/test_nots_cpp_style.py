import json
import unittest

import lib
import test_nots


def workspace(lint_context="TS_PROTO_PREPARE_DEPS", suppression="", automatic=False):
    files = test_nots.NotsTests().project()
    files["web/package.json"] = json.dumps({"dependencies": {"api": "workspace:../services/wire"}})
    files["build/conf/autoincludes.json"] = '["services"]'
    files["services/linters.make.inc"] = (
        f"IF({lint_context})\nSTYLE_CPP(CONFIG_TYPE .clang-format)\nENDIF()\n"
    )
    files["services/.clang-format"] = "BasedOnStyle: LLVM\n"
    files["services/wire/ya.make"] = (
        "PROTO_LIBRARY()\nINCLUDE_TAGS(TS_PROTO TS_PREPARE_DEPS)\n"
        f"{suppression}SRCS(service.proto)\nEND()\n"
    )
    files["services/wire/service.proto"] = 'syntax = "proto3";\nmessage Service {}\n'
    files["services/wire/package.json"] = "{}"
    if automatic:
        del files["services/wire/package.json"]
        deps = "library/typescript/ts-proto-deps"
        files[f"{deps}/ya.make"] = "TS_PACKAGE()\nEND()\n"
        files[f"{deps}/package.json"] = "{}"
    files["build/platform/clang/clang-format/ya.make"] = (
        "RESOURCES_LIBRARY()\nDECLARE_EXTERNAL_RESOURCE(CLANG_FORMAT sbr:1)\nEND()\n"
    )
    lib.tool_program(files, "contrib/tools/protoc", "protoc")
    return files


class NotsCppStyleTest(unittest.TestCase):
    def test_proto_preparation_emits_empty_cpp_check(self):
        for automatic in (False, True):
            for context in ("TS_PROTO_PREPARE_DEPS", "TS_PROTO OR TS_PROTO_PREPARE_DEPS"):
                with self.subTest(context=context, automatic=automatic):
                    self.assert_empty_check(workspace(context, automatic=automatic))

    def assert_empty_check(self, files):
        graph = lib.make(files, "web", "-t", "-DADD_PEERDIRS_GEN_TESTS=yes")
        run = lib.node_by_output(graph, "$(B)/services/wire/test-results/clang_format/meta.json")
        self.assertCountEqual(run["inputs"], [
            "$(S)/services/.clang-format", "$(S)/services/wire", "$(S)/tools/cpp_style_checker/wrapper.py",
        ])
        self.assertEqual(run["deps"], [lib.node_by_output(graph, "$(B)/common_test.context")["uid"]])
        command = run["cmds"][0]["cmd_args"]
        self.assertEqual(command[-4:], ["--config", "$(S)/services/.clang-format", "--global-resource",
                                       "CLANG_FORMAT_RESOURCE_GLOBAL::$(B)/resources/CLANG_FORMAT"])
        self.assertIn("$(B)/services/wire/output.tar", [out for n in graph["graph"] for out in n["outputs"]])
        self.assertFalse(any(out.endswith((".pb.cc", ".pb.h")) for n in graph["graph"] for out in n["outputs"]))

    def test_empty_check_respects_actual_context_and_exclusions(self):
        for files, flags in (
            (workspace("TS_PROTO"), []),
            (workspace(suppression="NO_LINT()\n"), []),
            (workspace(), ["-DCPP_ANALYSIS_MODE=yes"]),
        ):
            with self.subTest(flags=flags, module=files["services/wire/ya.make"], linter=files["services/linters.make.inc"]):
                graph = lib.make(files, "web", "-t", "-DADD_PEERDIRS_GEN_TESTS=yes", *flags)
                self.assertFalse(any("$(B)/services/wire/test-results/clang_format/meta.json" in n["outputs"]
                                     for n in graph["graph"]))


if __name__ == "__main__":
    unittest.main()
