import base64
import unittest

import lib
from test_go_tests import go_test_world
from test_java_tests import junit_tree
from test_link_test_nodes import fixture


class SharedTestNodesTest(unittest.TestCase):
    def test_depends_uses_archive_producer_discovered_without_linking(self):
        files = {
            "root/ya.make": f"LIBRARY()\n{lib.BARE_MODULE}END()\nRECURSE(../lib ../tests)\n",
            "lib/ya.make": f"LIBRARY()\n{lib.BARE_MODULE}PEERDIR(native)\nEND()\n",
            "native/ya.make": f"LIBRARY()\n{lib.BARE_MODULE}COPY_FILE(prebuilt.a native.a)\nEND()\n",
            "native/prebuilt.a": "",
            "tool/ya.make": f"PROGRAM()\n{lib.BARE_MODULE}PEERDIR(native)\nEND()\n",
            "tests/ya.make": "EXECTEST()\nRUN(tool)\nDEPENDS(tool)\nEND()\n",
        }
        graph = lib.make(files, "root", "-t")
        copied = lib.node_by_output(graph, "$(B)/native/native.a")
        archive = lib.node_by_output(graph, "$(B)/native/libnative.a")
        self.assertIn(copied["uid"], archive["deps"])
        self.assertEqual(sum("$(B)/native/native.a" in n["outputs"] for n in graph["graph"]), 1)
        run = lib.node_by_output(graph, "$(B)/tests/test-results/exectest/meta.json")
        tool = lib.node_by_output(graph, "$(B)/tool/tool")
        self.assertIn(tool["uid"], run["deps"])

    def test_go_and_java_use_recipes_data_and_retry_aggregation(self):
        macros = "USE_RECIPE(recipe start)\nDATA(sbr://123=data sbr://123=other)\nSPLIT_FACTOR(2)\n"
        go = go_test_world()
        go["go/ya.make"] = f"GO_TEST()\nGO_TEST_SRCS(a_test.go)\n{macros}END()\n"
        go["go/a_test.go"] = "package example\n"
        java = junit_tree(f"JUNIT5()\n{macros}END()\n")
        for files, target, suite in ((go, "go", "go"), (java, "proj/sub/ut", "sub-ut")):
            with self.subTest(suite=suite):
                graph = lib.make(files, target, "-t", "--tests-retries", "2")
                work = f"$(B)/{target}/test-results/{suite}"
                download = lib.node_by_output(graph, "$(B)/sandbox-storage/123/resource")
                if suite == "go":
                    self.assertEqual(download["requirements"], {"cpu": 1, "ram": 32, "network": "full"})
                context = lib.node_by_output(graph, "$(B)/common_test.context")
                for retry in (1, 2):
                    runs = []
                    for index in (0, 1):
                        node = lib.node_by_output(graph, f"{work}/run{retry}/chunk{index}/meta.json")
                        runs.append(node)
                        args = node["cmds"][-1]["cmd_args"]
                        self.assertEqual(base64.b64decode(args[args.index("--recipes") + 1]), b"recipe start \n")
                        self.assertEqual(args[args.index("--retry") + 1], str(retry))
                        self.assertEqual(args[args.index("--modulo") + 1], "2")
                        self.assertEqual(args[args.index("--modulo-index") + 1], str(index))
                        self.assertIn(download["uid"], node["deps"])
                        self.assertIn(context["uid"], node["deps"])
                    accumulator = lib.node_by_output(graph, f"{work}/run{retry}/meta.json")
                    self.assertEqual(set(accumulator["deps"]), {n["uid"] for n in runs})
                merger = lib.node_by_output(graph, f"{work}/meta.json")
                self.assertEqual(merger["kv"]["p"], "TR")
                self.assertIn(merger["uid"], graph["result"])
                self.assertEqual(sum("$(B)/sandbox-storage/123/resource" in n["outputs"] for n in graph["graph"]), 1)

    def test_detekt_resource_from_transitive_autoinclude(self):
        files = fixture()
        files["build/conf/autoincludes.json"] = '["peer"]\n'
        files["peer/linters.make.inc"] = "STYLE_DETEKT()\n"
        files["peer/ya.make"] = "LIBRARY()\n" + lib.BARE_MODULE + "END()\n"
        files["lib/ya.make"] = files["lib/ya.make"].replace("END()", "PEERDIR(peer)\nEND()")
        files["build/platform/java/detekt/ya.make"] = "RESOURCES_LIBRARY()\nDECLARE_EXTERNAL_RESOURCE(DETEKT sbr:123)\nEND()\n"
        graph = lib.make(files, "lib/ut", "-t")
        node = lib.node_by_output(graph, "$(B)/lib/ut/test-results/unittest/meta.json")
        args = node["cmds"][0]["cmd_args"]
        self.assertEqual(sum(arg.startswith("DETEKT_RESOURCE_GLOBAL::") for arg in args), 1)

    def test_detekt_resource_peer_without_kotlin_sources(self):
        for setting, explicit_peer, expected in (
            ("", False, False),
            ("STYLE_DETEKT()\n", False, True),
            ("STYLE_DETEKT()\n", True, True),
            ("NO_LINT()\nSTYLE_DETEKT()\n", False, False),
            ("NO_LINT(none_internal)\nSTYLE_DETEKT()\n", False, False),
            ("SET(TIDY yes)\nSTYLE_DETEKT()\n", False, False),
            ("SET(_NO_LINT_VALUE none)\nSTYLE_DETEKT()\n", False, False),
            ("SET(_NO_LINT_VALUE none_internal)\nSTYLE_DETEKT()\n", False, False),
            ("NO_LINT(ktlint)\nSTYLE_DETEKT()\n", False, True),
        ):
            with self.subTest(setting=setting, explicit_peer=explicit_peer):
                files = fixture()
                peer = "PEERDIR(build/platform/java/detekt)\n" if explicit_peer else ""
                files["lib/ut/ya.make"] = files["lib/ut/ya.make"].replace("END()", setting + peer + "END()")
                files["build/platform/java/detekt/ya.make"] = "RESOURCES_LIBRARY()\nDECLARE_EXTERNAL_RESOURCE(DETEKT sbr:123)\nEND()\n"
                graph = lib.make(files, "lib/ut", "-t")
                node = lib.node_by_output(graph, "$(B)/lib/ut/test-results/unittest/meta.json")
                args = node["cmds"][0]["cmd_args"]
                self.assertEqual(sum(arg.startswith("DETEKT_RESOURCE_GLOBAL::") for arg in args), int(expected))

    def test_cpp_uses_shared_configuration_and_inherited_environment(self):
        files = fixture()
        files["lib/ut/ya.make"] = files["lib/ut/ya.make"].replace("END()", "STYLE_CPP()\nEND()")
        files["build/platform/clang/clang-format/ya.make"] = "RESOURCES_LIBRARY()\nEND()\n"
        files["ya.conf"] = 'use_command_file_in_testtool = false\noutput_style = "jsonl"\n'
        graph = lib.make(files, "lib/ut", "-t", env={"ASAN_OPTIONS": "custom=1"})
        for suite in ("unittest", "clang_format"):
            node = lib.node_by_output(graph, f"$(B)/lib/ut/test-results/{suite}/meta.json")
            args = node["cmds"][-1]["cmd_args"]
            self.assertNotIn("--ya-start-command-file", args)
            self.assertEqual(args[args.index("--output-style") + 1], "jsonl")
            self.assertEqual(node["env"]["ASAN_OPTIONS"], "custom=1:exitcode=100")


if __name__ == "__main__":
    unittest.main(verbosity=2)
