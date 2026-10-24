import unittest

import lib
from test_go_cgo import cgo_world
from test_go_tests import (
    AUTOCHECK, args_after, between, go_module, go_test_world,
    go_tool_args, go_world, resources_library, test_node,
)


PEER_TESTS = ("--target-platform-flag", "ADD_PEERDIRS_GEN_TESTS=yes")


def suite_resources(graph, project):
    return args_after(test_node(graph, project)["cmds"][0]["cmd_args"], "--global-resource")


def fmt_files(graph, project):
    suites = [node for node in graph["graph"] if node["kv"].get("path") == project + "/gofmt"]
    if len(suites) != 1:
        raise AssertionError(f"expected one gofmt suite for {project}, got {len(suites)}")
    args = suites[0]["cmds"][0]["cmd_args"]
    start = args.index("run_go_fmt")
    end = -1 if args[-1] == "--ya-end-command-file" else len(args)
    files = args[args.index("--out-path", start) + 2:end]
    if args_after(args, "--test-related-path") != files:
        raise AssertionError("gofmt related paths differ from command files")
    if suites[0]["inputs"] != sorted(["$(S)/" + project, *files]):
        raise AssertionError("gofmt inputs differ from command files")
    return files


class GoFmtSourcesTest(unittest.TestCase):
    def test_shared_source_directory_keeps_the_first_peer_dart_resources(self):
        files = go_world()
        resources_library(files, "resources/crown", "CROWN_PAYLOAD")
        resources_library(files, "resources/roots", "ROOT_PAYLOAD")
        go_module(files, "entry", macros="PEERDIR(crown)\n")
        files["crown/ya.make"] = (
            "GO_LIBRARY()\nSRCDIR(foliage)\nSRCS(upper.go)\n"
            "PEERDIR(roots resources/crown)\nEND()\n")
        files["roots/ya.make"] = (
            "GO_LIBRARY()\nSRCDIR(foliage)\nSRCS(lower.go)\n"
            "PEERDIR(resources/roots)\nEND()\n")
        files["foliage/upper.go"] = files["foliage/lower.go"] = "package foliage\n"
        for targets in (("entry",), ("crown",), ("crown", "roots"), ("roots", "crown")):
            with self.subTest(targets=targets):
                graph = lib.make(files, *targets, *AUTOCHECK, *PEER_TESTS)
                self.assertEqual(fmt_files(graph, "foliage"), ["$(S)/foliage/lower.go", "$(S)/foliage/upper.go"])
                self.assertEqual(suite_resources(graph, "foliage/gofmt"), suite_resources(graph, "roots/govet"))
                root_payload = "ROOT_PAYLOAD_RESOURCE_GLOBAL::$(B)/resources/ROOT_PAYLOAD"
                crown_payload = "CROWN_PAYLOAD_RESOURCE_GLOBAL::$(B)/resources/CROWN_PAYLOAD"
                self.assertIn(root_payload, suite_resources(graph, "foliage/gofmt"))
                self.assertNotIn(crown_payload, suite_resources(graph, "foliage/gofmt"))
                for required in (root_payload, crown_payload,
                                 "GO_TOOLS_RESOURCE_GLOBAL::$(B)/resources/GO_TOOLS"):
                    self.assertIn(required, suite_resources(graph, "crown/govet"))
        graph = lib.make(files, "crown", *AUTOCHECK)
        self.assertEqual(fmt_files(graph, "foliage"), ["$(S)/foliage/upper.go"])
        self.assertIn(crown_payload, args_after(
            test_node(graph, "foliage/gofmt")["cmds"][0]["cmd_args"], "--global-resource"))

    def test_ros_contexts_preserve_go_and_native_resources_before_check_merging(self):
        from test_ros import ros_files
        files = go_world()
        files.update(ros_files())
        resources_library(files, "build/platform/clang", "CLANG20")
        files["build/conf/autoincludes.json"] = '["orchard"]\n'
        files["orchard/linters.make.inc"] = (
            "IF(MODULE_LANG == CPP)\nSTYLE_CPP()\n"
            "SET_CUSTOM_CLANG_TIDY(resources/analyzer ORCHARD_ANALYZER)\nENDIF()\n")
        resources_library(files, "resources/analyzer", "ORCHARD_ANALYZER")
        resources_library(files, "resources/go", "GO_PAYLOAD")
        resources_library(files, "resources/common", "COMMON_PAYLOAD")
        resources_library(files, "resources/crown", "CROWN_PAYLOAD")
        files["orchard/messages/ya.make"] = (
            f"ROS_LIBRARY(fruit)\n{lib.BARE_MODULE}PEERDIR(resources/common)\n"
            "IF(GO_ROS)\nSRCDIR(orchard/foliage)\nSRCS(lower.go)\nPEERDIR(resources/go)\n"
            "ELSEIF(CPP_ROS)\nSRCS(native.cpp)\nENDIF()\nEND()\n")
        files["orchard/messages/native.cpp"] = "int native;\n"
        files["orchard/crown/ya.make"] = (
            "GO_LIBRARY()\nSRCDIR(orchard/foliage)\nSRCS(upper.go)\n"
            "PEERDIR(orchard/messages resources/crown)\nEND()\n")
        files["orchard/foliage/upper.go"] = files["orchard/foliage/lower.go"] = "package foliage\n"
        files["orchard/native/ya.make"] = (
            f"PROGRAM()\n{lib.BARE_MODULE}ALLOCATOR(FAKE)\nPEERDIR(orchard/messages)\nSRCS(entry.cpp)\nEND()\n")
        files["orchard/native/entry.cpp"] = "int entry;\n"
        for targets in (("orchard/native", "orchard/crown"), ("orchard/crown", "orchard/native")):
            with self.subTest(targets=targets):
                graph = lib.make(files, *targets, *AUTOCHECK, *PEER_TESTS)
                self.assertEqual(fmt_files(graph, "orchard/foliage"), [
                    "$(S)/orchard/foliage/lower.go", "$(S)/orchard/foliage/upper.go"])
                checking = suite_resources(graph, "orchard/foliage/gofmt")
                self.assertEqual(checking, suite_resources(graph, "orchard/messages/govet"))
                for name in ("GO_PAYLOAD", "COMMON_PAYLOAD", "GO_TOOLS", "CLANG20"):
                    self.assertIn(f"{name}_RESOURCE_GLOBAL::$(B)/resources/{name}", checking)
                self.assertNotIn("CROWN_PAYLOAD_RESOURCE_GLOBAL::$(B)/resources/CROWN_PAYLOAD", checking)
                self.assertIn("CROWN_PAYLOAD_RESOURCE_GLOBAL::$(B)/resources/CROWN_PAYLOAD",
                              suite_resources(graph, "orchard/crown/govet"))
                for project in ("orchard/native", "orchard/messages"):
                    self.assertIn("CLANG_FORMAT_RESOURCE_GLOBAL::$(B)/resources/CLANG_FORMAT",
                                  suite_resources(graph, project + "/clang_format"))
                compiled = next(node for node in graph["graph"] if node["kv"]["p"] == "CC"
                                and "$(S)/orchard/messages/native.cpp" in node["inputs"])
                command = compiled["cmds"][0]["cmd_args"]
                self.assertIn("$(B)/resources/CLANG20/bin/clang++", command)
                self.assertFalse(any("$(B)/resources/ORCHARD_ANALYZER" in node["outputs"]
                                     for node in graph["graph"]))
                self.assertNotIn("ORCHARD_ANALYZER_RESOURCE_GLOBAL::$(B)/resources/ORCHARD_ANALYZER", checking)

    def test_raw_declared_sources_are_sorted_deduplicated_and_grouped_by_directory(self):
        files = go_world()
        files["pkg/ya.make"] = (
            "GO_LIBRARY()\nSRCDIR(shared)\n"
            "COPY_FILE(seed.txt generated.go)\n"
            "SRCS(z.go a.go a.go ${ARCADIA_ROOT}/pkg/a.go os_windows.go ${BINDIR}/generated.go "
            "other.go ${ARCADIA_ROOT}/vendor/check/v.go)\n"
            "IF(OS_LINUX)\nSRCS(linux.go)\nELSE()\nSRCS(darwin.go)\nENDIF()\nEND()\n"
        )
        for name in ("z.go", "a.go", "linux.go", "darwin.go"):
            files["pkg/" + name] = "package pkg\n"
        files["pkg/seed.txt"] = "package pkg\n"
        files["pkg/os_windows.go"] = "//go:build windows\n\npackage pkg\n"
        files["shared/other.go"] = "package pkg\n"
        files["vendor/check/v.go"] = "package pkg\n"
        graph = lib.make(files, "pkg", *AUTOCHECK)
        self.assertEqual(fmt_files(graph, "pkg"), [
            "$(S)/pkg/a.go", "$(S)/pkg/linux.go", "$(S)/pkg/os_windows.go", "$(S)/pkg/z.go",
        ])
        self.assertEqual(fmt_files(graph, "shared"), ["$(S)/shared/other.go"])

    def test_all_test_sources_are_checked_in_peer_libraries_without_importing_tests(self):
        files = go_world()
        go_module(files, "root", module="GO_PROGRAM", macros="PEERDIR(leaf explicit)\n")
        files["leaf/ya.make"] = "GO_LIBRARY()\nALL_GO_SRCS()\nALL_GO_TEST_SRCS()\nEND()\n"
        files["explicit/ya.make"] = (
            "GO_LIBRARY()\nSRCS(explicit.go)\nGO_TEST_SRCS(ignored_test.go)\n"
            "GO_XTEST_SRCS(ignored_external_test.go)\nEND()\n"
        )
        for name in ("z.go", "a.go", "z_test.go", "a_test.go"):
            files["leaf/" + name] = 'package leaf\nimport "a.yandex-team.ru/test_only"\n' if name.endswith("_test.go") else "package leaf\n"
        for name in ("explicit.go", "ignored_test.go", "ignored_external_test.go"):
            files["explicit/" + name] = "package explicit\n"
        go_module(files, "test_only")
        graph = lib.make(files, "root", *AUTOCHECK, *PEER_TESTS)
        self.assertEqual(fmt_files(graph, "leaf"), [
            "$(S)/leaf/a.go", "$(S)/leaf/a_test.go", "$(S)/leaf/z.go", "$(S)/leaf/z_test.go",
        ])
        self.assertEqual(fmt_files(graph, "explicit"), ["$(S)/explicit/explicit.go"])
        package = lib.node_by_output(graph, "$(B)/leaf/leaf.a")
        self.assertEqual(between(go_tool_args(package), "++srcs", "++asm-flags"), ["$(S)/leaf/a.go", "$(S)/leaf/z.go"])
        self.assertFalse(any("$(B)/test_only/test_only.a" in node["outputs"] for node in graph["graph"]))

    def test_globbed_cgo_originals_are_checked_without_generated_go_outputs(self):
        files = cgo_world()
        files["pkg/ya.make"] = "GO_LIBRARY()\nALL_GO_SRCS()\nCGO_SRCS(native.go)\nEND()\n"
        files["pkg/plain.go"] = "package pkg\n"
        files["pkg/native.go"] = 'package pkg\nimport "C"\n'
        graph = lib.make(files, "pkg", *AUTOCHECK)
        self.assertEqual(fmt_files(graph, "pkg"), ["$(S)/pkg/native.go", "$(S)/pkg/plain.go"])

    def test_metadata_only_go_peers_keep_their_declared_checking_sources(self):
        files = go_world()
        files["root/ya.make"] = "UNION()\nPEERDIR(pkg)\nEND()\n"
        go_module(files, "pkg", srcs="z.go a.go")
        files["pkg/z.go"] = files["pkg/a.go"] = "package pkg\n"
        graph = lib.make(files, "root", *AUTOCHECK, *PEER_TESTS)
        self.assertEqual(fmt_files(graph, "pkg"), ["$(S)/pkg/a.go", "$(S)/pkg/z.go"])
        without_tests = lib.make(files, "root", *PEER_TESTS)
        self.assertFalse(any(node["kv"].get("run_test_node") for node in without_tests["graph"]))

    def test_test_for_resolves_test_files_and_keys_the_suite_by_their_source_directory(self):
        files = go_test_world()
        files["pkg/ya.make"] = (
            "GO_LIBRARY()\nSRCS(prod.go)\nGO_TEST_SRCS(z_test.go a_test.go)\n"
            "GO_XTEST_SRCS(external_test.go)\nEND()\nRECURSE_FOR_TESTS(gotest)\n"
        )
        for name in ("prod.go", "z_test.go", "a_test.go", "external_test.go"):
            files["pkg/" + name] = "package pkg\n"
        files["pkg/gotest/ya.make"] = "GO_TEST_FOR(pkg)\nEND()\n"
        for targets in (("pkg/gotest",), ("pkg",), ("pkg/gotest", "pkg"), ("pkg", "pkg/gotest")):
            with self.subTest(targets=targets):
                graph = lib.make(files, *targets, *AUTOCHECK)
                expected = ["$(S)/pkg/a_test.go", "$(S)/pkg/external_test.go", "$(S)/pkg/z_test.go"]
                if "pkg" in targets:
                    expected.insert(2, "$(S)/pkg/prod.go")
                self.assertEqual(fmt_files(graph, "pkg"), expected)

    def test_all_test_sources_also_compile_in_a_standalone_test_module(self):
        files = go_test_world()
        files["pkg/ya.make"] = "GO_TEST()\nALL_GO_TEST_SRCS()\nEND()\n"
        files["pkg/a_test.go"] = "package pkg\n"
        files["pkg/z_test.go"] = "package pkg\n"
        graph = lib.make(files, "pkg", *AUTOCHECK)
        self.assertEqual(fmt_files(graph, "pkg"), ["$(S)/pkg/a_test.go", "$(S)/pkg/z_test.go"])
        binary = lib.node_by_output(graph, "$(B)/pkg/pkg")
        self.assertEqual(between(go_tool_args(binary), "++test_srcs", "++xtest_srcs"), [
            "$(S)/pkg/a_test.go", "$(S)/pkg/z_test.go",
        ])


if __name__ == "__main__":
    unittest.main(verbosity=2)
