import unittest

from tst import lib
from tst.make.test_yql import BARE, udf_fixture


class YqlPrebuiltContextsTest(unittest.TestCase):
    def fixture(self):
        files = udf_fixture()
        files.update({
            "extensions/comet/ya.make": (
                "YQL_UDF_CONTRIB(CometStats)\n"
                "SET(COMETSTATS_RESOURCE_DEFAULT 731)\n"
                "SET(COMETSTATS_RESOURCE_ASAN 732)\n"
                "SET(COMETSTATS_RESOURCE_MSAN 733)\n"
                "SET(COMETSTATS_RESOURCE_TSAN 734)\n"
                "SET(COMETSTATS_RESOURCE_UBSAN 735)\n"
                "SET(VALIDATE_DATA yes)\nSTYLE_CPP()\n"
                "SRCS(aggregate.cpp)\nPEERDIR(source_peer)\nEND()\n"
            ),
            "extensions/comet/aggregate.cpp": "int aggregate;\n",
            "source_peer/ya.make": f"LIBRARY()\n{BARE}STYLE_CPP()\nSRCS(peer.cpp)\nEND()\n",
            "source_peer/peer.cpp": "int peer;\n",
            "build/scripts/fs_tools.py": "import process_command_files\n",
            "build/scripts/process_command_files.py": "",
            "build/platform/clang/clang-format/ya.make": (
                "RESOURCES_LIBRARY()\nDECLARE_EXTERNAL_RESOURCE(CLANG_FORMAT sbr:11)\nEND()\n"
            ),
            "build/platform/python/ymake_python3/ya.make": (
                "RESOURCES_LIBRARY()\nDECLARE_EXTERNAL_RESOURCE(YMAKE_PYTHON3 sbr:12)\nEND()\n"
            ),
        })
        return files

    def test_shared_contrib_uses_selected_prebuilt_and_preserves_plugin_and_checks(self):
        for sanitizer, resource in (("", "731"), ("address", "732"), ("memory", "733"),
                                    ("thread", "734"), ("undefined", "735")):
            with self.subTest(sanitizer=sanitizer):
                graph = lib.make(self.fixture(), "extensions/comet", "--target-platform-tests",
                                 "-DADD_PEERDIRS_GEN_TESTS=yes", f"-DSANITIZER_TYPE={sanitizer}",
                                 opensource=False)
                out = "$(B)/extensions/comet/libCometStats.so"
                fetch = lib.node_by_output(graph, out + ".prebuilt")
                args = fetch["cmds"][0]["cmd_args"]
                self.assertEqual(args[args.index("--resource-id") + 1], resource)
                self.assertIn("--executable", args)
                self.assertEqual(args[args.index("--") + 1:], ["libCometStats.so.prebuilt", "--ya-end-command-file"])
                copy = lib.node_by_output(graph, out)
                plugin_path = ("$(B)/extensions/comet/"
                               "yql/essentials/public/udf/service/drop_policy_globals.py.pyplugin")
                plugin_refs = {n["uid"] for n in graph["graph"] if plugin_path in n["outputs"]}
                self.assertEqual(copy["cmds"], [{
                    "cmd_args": ["$(B)/resources/YMAKE_PYTHON3/bin/python3",
                                 "$(S)/build/scripts/fs_tools.py", "copy", out + ".prebuilt", out],
                    "env": {"ARCADIA_ROOT_DISTBUILD": "$(S)"},
                }])
                self.assertEqual(copy["kv"], {"p": "ld", "pc": "light-blue", "show_out": "yes"})
                self.assertIn(fetch["uid"], copy["deps"])
                self.assertEqual(len(plugin_refs.intersection(copy["deps"])), 1)
                python = lib.node_by_output(graph, "$(B)/resources/YMAKE_PYTHON3")
                self.assertIn(python["uid"], fetch["deps"])
                self.assertIn(python["uid"], copy["deps"])
                self.assertEqual(sorted(copy["inputs"]), sorted([
                    out + ".prebuilt", plugin_path, "$(S)/build/scripts/fs_tools.py",
                    "$(S)/build/scripts/process_command_files.py",
                ]))
                style = lib.node_by_output(graph, "$(B)/extensions/comet/test-results/clang_format/meta.json")
                self.assertEqual(style["inputs"], ["$(S)/build/config/tests/cpp_style/.clang-format",
                                                  "$(S)/extensions/comet", "$(S)/tools/cpp_style_checker/wrapper.py"])
                validation = lib.node_by_output(graph, "$(B)/extensions/comet/test-results/validate_resource/meta.json")
                self.assertEqual(validation["inputs"], ["$(S)/extensions/comet", resource])
                bindings = [validation["cmds"][0]["cmd_args"][i + 1]
                            for i, arg in enumerate(validation["cmds"][0]["cmd_args"]) if arg == "--global-resource"]
                self.assertEqual(bindings, ["CLANG_FORMAT_RESOURCE_GLOBAL::$(B)/resources/CLANG_FORMAT",
                                            "YMAKE_PYTHON3_RESOURCE_GLOBAL::$(B)/resources/YMAKE_PYTHON3"])
                outputs = [o for n in graph["graph"] for o in n["outputs"]]
                self.assertFalse(any(o.endswith("aggregate.cpp.pic.o") or "/source_peer/" in o
                                     or o.endswith("/udf_probe") for o in outputs))

    def test_multimodule_style_records_union_files_before_emitting_one_suite(self):
        files = self.fixture()
        files["extensions/comet/ya.make"] = (
            f"YQL_UDF(CometStats)\n{BARE}UDF_NO_PROBE()\nSTYLE_CPP()\n"
            'IF (MODULE_TAG == "YQL_UDF_SHARED")\nSRCS(shared.cpp)\n'
            'ELSEIF (MODULE_TAG == "YQL_UDF_STATIC_CURRENT")\nSRCS(current.cpp)\n'
            "ELSE()\nSRCS(stable.cpp)\nENDIF()\nEND()\n"
        )
        for name in ("shared", "stable", "current"):
            files[f"extensions/comet/{name}.cpp"] = f"int {name};\n"
        graph = lib.make(files, "extensions/comet", "--target-platform-tests")
        suites = [n for n in graph["graph"] if n.get("kv", {}).get("path") == "extensions/comet/clang_format"]
        self.assertEqual(len(suites), 1)
        args = suites[0]["cmds"][0]["cmd_args"]
        checked = ["$(S)/build/scripts/c_templates/svn_interface.c",
                   *[f"$(S)/extensions/comet/{name}.cpp" for name in ("current", "shared", "stable")]]
        self.assertEqual(args[args.index("--config") + 2:], [
            "--global-resource", "CLANG_FORMAT_RESOURCE_GLOBAL::$(B)/resources/CLANG_FORMAT", *checked,
        ])
        for file in checked:
            self.assertIn(file, suites[0]["inputs"])

    def test_prebuilt_selection_does_not_replace_stable_or_current_static_peers(self):
        files = self.fixture()
        files["extensions/comet/ya.make"] = files["extensions/comet/ya.make"].replace(
            "SRCS(aggregate.cpp)",
            "FROM_SANDBOX(FILE 791 OUT_NOAUTO ${MODULE_TAG}.payload)\n"
            "RESOURCE(${BINDIR}/${MODULE_TAG}.payload /payload)\nSRCS(aggregate.cpp)",
        )
        for name, abi in (("stable", ""), ("current", "YQL_CURRENT_ABI_VERSION()\n")):
            files[f"runners/{name}/ya.make"] = (
                f"PROGRAM()\n{BARE}{abi}PEERDIR(extensions/comet)\nEND()\n"
            )
        for targets in (("extensions/comet", "runners/current", "runners/stable"),
                        ("runners/stable", "runners/current", "extensions/comet")):
            with self.subTest(targets=targets):
                graph = lib.make(files, *targets, opensource=False)
                shared = lib.node_by_output(graph, "$(B)/extensions/comet/libCometStats.so")
                self.assertEqual(shared["kv"]["p"], "ld")
                for abi, suffix, archive in (
                    ("stable", "udfs", "libCometStats.global.a"),
                    ("current", "udfs.abi_current", "yql-abi-current-CometStats.global.a"),
                ):
                    compiled = lib.node_by_output(graph, f"$(B)/extensions/comet/aggregate.cpp.{suffix}.o")
                    args = compiled["cmds"][0]["cmd_args"]
                    self.assertNotIn("-DBUILD_UDF", args)
                    self.assertEqual("-DUSE_CURRENT_UDF_ABI_VERSION" in args, abi == "current")
                    static = lib.node_by_output(graph, "$(B)/extensions/comet/" + archive)
                    self.assertIn(compiled["uid"], static["deps"])
                    runner = lib.node_by_output(graph, f"$(B)/runners/{abi}/{abi}")
                    self.assertIn(static["uid"], runner["deps"])
                    self.assertNotIn(shared["uid"], runner["deps"])
                for tag in ("YQL_UDF_SHARED", "YQL_UDF_STATIC", "YQL_UDF_STATIC_CURRENT"):
                    payload = f"$(B)/extensions/comet/{tag}.payload"
                    producer = lib.node_by_output(graph, payload)
                    resources = [n for n in graph["graph"] if payload in n["inputs"]]
                    self.assertTrue(resources)
                    for resource in resources:
                        self.assertIn(producer["uid"], resource["deps"])
                        self.assertFalse(any(other != tag and f"/{other}.payload" in item
                                             for other in ("YQL_UDF_SHARED", "YQL_UDF_STATIC", "YQL_UDF_STATIC_CURRENT")
                                             for item in resource["inputs"]))

    def test_source_fallback_uses_deferred_declarations_and_implicit_udf_peers(self):
        for flags, opensource in (([], True), (["-DBUILD_YQL_FROM_SOURCES=yes"], False),
                                  (["-DSANITIZER_TYPE=unsupported"], False),
                                  (["-DCOMETSTATS_RESOURCE_DEFAULT=None"], False),
                                  (["-DCOMETSTATS_RESOURCE_DEFAULT="], False)):
            with self.subTest(flags=flags, opensource=opensource):
                files = self.fixture()
                files["extensions/comet/ya.make"] = files["extensions/comet/ya.make"].replace(
                    "SET(COMETSTATS_RESOURCE_DEFAULT 731)", "DEFAULT(COMETSTATS_RESOURCE_DEFAULT 731)",
                )
                graph = lib.make(files, "extensions/comet", *flags, opensource=opensource)
                obj = lib.node_by_output(graph, "$(B)/extensions/comet/aggregate.cpp.pic.o")
                shared = lib.node_by_output(graph, "$(B)/extensions/comet/libCometStats.so")
                self.assertIn(obj["uid"], shared["deps"])
                self.assertIn("-DBUILD_UDF", obj["cmds"][0]["cmd_args"])
                for peer in ("source_peer", "yql/essentials/public/udf", "yql/essentials/public/udf/support"):
                    self.assertTrue(any(o.startswith(f"$(B)/{peer}/") for n in graph["graph"] for o in n["outputs"]))
                self.assertFalse(any(o.endswith("libCometStats.so.prebuilt") for n in graph["graph"] for o in n["outputs"]))

    def test_default_udf_name_selects_the_resource_for_its_two_directory_name(self):
        files = self.fixture()
        files["extensions/comet/ya.make"] = (
            "YQL_UDF_CONTRIB()\nSET(EXTENSIONS-COMET_RESOURCE_DEFAULT 761)\n"
            "SRCS(missing.cpp)\nPEERDIR(missing/peer)\nEND()\n"
        )
        graph = lib.make(files, "extensions/comet", opensource=False)
        fetch = lib.node_by_output(graph, "$(B)/extensions/comet/libextensions-comet.so.prebuilt")
        args = fetch["cmds"][0]["cmd_args"]
        self.assertEqual(args[args.index("--resource-id") + 1], "761")
        lib.node_by_output(graph, "$(B)/extensions/comet/libextensions-comet.so")

    def test_merged_style_files_determine_split_and_keep_first_record_metadata(self):
        files = self.fixture()
        files["extensions/comet/ya.make"] = (
            f"YQL_UDF(CometStats)\n{BARE}UDF_NO_PROBE()\n"
            "PEERDIR(resources/${MODULE_TAG})\n"
            "_ADD_CUSTOM_EXPLICIT_LINTER_CHECK(NAME payload_check WRAPPER_SCRIPT tools/check.py "
            "FILES ${MODULE_TAG}.txt FILE_PROCESSING_TIME 40 EXTRA_PARAMS flavor=${MODULE_TAG} "
            "DEFAULT_CONFIGS configs/${MODULE_TAG}.json)\nEND()\n"
        )
        for tag in ("YQL_UDF_SHARED", "YQL_UDF_STATIC", "YQL_UDF_STATIC_CURRENT"):
            files[f"configs/{tag}.json"] = f'{{"payload_check": "configs/{tag}.rules"}}'
            files[f"configs/{tag}.rules"] = ""
            files[f"resources/{tag}/ya.make"] = (
                f"RESOURCES_LIBRARY()\nDECLARE_EXTERNAL_RESOURCE(CONTEXT_{tag} sbr:891)\nEND()\n"
            )
        graph = lib.make(files, "extensions/comet", "--target-platform-tests")
        suites = [n for n in graph["graph"] if n.get("kv", {}).get("path") == "extensions/comet/payload_check"]
        self.assertEqual(len(suites), 2)
        checked = []
        for suite in suites:
            args = suite["cmds"][0]["cmd_args"]
            self.assertEqual([args[i + 1] for i, arg in enumerate(args) if arg == "--extra-param"],
                             ["flavor=YQL_UDF_STATIC"])
            self.assertEqual([args[i + 1] for i, arg in enumerate(args) if arg == "--config"],
                             ["$(S)/configs/YQL_UDF_STATIC.rules"])
            self.assertNotIn("$(S)/configs/YQL_UDF_SHARED.rules", suite["inputs"])
            self.assertNotIn("$(S)/configs/YQL_UDF_STATIC_CURRENT.rules", suite["inputs"])
            bindings = [args[i + 1] for i, arg in enumerate(args) if arg == "--global-resource"]
            self.assertIn("CONTEXT_YQL_UDF_STATIC_RESOURCE_GLOBAL::$(B)/resources/CONTEXT_YQL_UDF_STATIC", bindings)
            self.assertFalse(any("CONTEXT_YQL_UDF_SHARED_RESOURCE_GLOBAL" in b
                                 or "CONTEXT_YQL_UDF_STATIC_CURRENT_RESOURCE_GLOBAL" in b for b in bindings))
            checked.extend(a for a in args[args.index("run_custom_lint"):] if a.endswith(".txt"))
        self.assertEqual(sorted(checked), [f"$(S)/extensions/comet/{tag}.txt" for tag in
                                         ("YQL_UDF_SHARED", "YQL_UDF_STATIC", "YQL_UDF_STATIC_CURRENT")])

    def test_style_identity_merges_an_empty_context_without_merging_distinct_checks(self):
        files = self.fixture()
        files["extensions/comet/ya.make"] = (
            f"YQL_UDF(CometStats)\n{BARE}UDF_NO_PROBE()\nSTYLE_CPP()\n"
            '_ADD_CUSTOM_EXPLICIT_LINTER_CHECK(NAME separate_check WRAPPER_SCRIPT tools/check.py FILES notes.txt)\n'
            'IF (MODULE_TAG != "YQL_UDF_STATIC")\nSRCS(aggregate.cpp)\nENDIF()\nEND()\n'
        )
        graph = lib.make(files, "extensions/comet", "--target-platform-tests")
        for name in ("clang_format", "separate_check"):
            suites = [n for n in graph["graph"] if n.get("kv", {}).get("path") == f"extensions/comet/{name}"]
            self.assertEqual(len(suites), 1)
            args = suites[0]["cmds"][0]["cmd_args"]
            checked = [a for a in args[args.index("run_custom_lint"):] if a.startswith("$(S)/extensions/comet/")]
            self.assertEqual(checked, ["$(S)/extensions/comet/aggregate.cpp"] if name == "clang_format"
                             else ["$(S)/extensions/comet/notes.txt"])

    def test_source_epilogue_keeps_declaration_values_and_peer_order(self):
        files = self.fixture()
        files["extensions/comet/ya.make"] = (
            f"YQL_UDF_CONTRIB(CometStats)\n{BARE}UDF_NO_PROBE()\n"
            "SET(SOURCE first.cpp)\nSRCS(${SOURCE})\nSET(SOURCE second.cpp)\nSRCS(${SOURCE})\n"
            "SET(PEER peers/first)\nPEERDIR(${PEER})\nSET(PEER peers/second)\nPEERDIR(${PEER})\nEND()\n"
        )
        for name in ("first", "second"):
            files[f"extensions/comet/{name}.cpp"] = f"int {name};\n"
            files[f"peers/{name}/ya.make"] = (
                f"LIBRARY()\n{BARE}LDFLAGS(GLOBAL -Wl,--{name}-peer)\nSRCS(peer.cpp)\nEND()\n"
            )
            files[f"peers/{name}/peer.cpp"] = "int peer;\n"
        graph = lib.make(files, "extensions/comet", opensource=False)
        link = lib.node_by_output(graph, "$(B)/extensions/comet/libCometStats.so")
        args = next(c["cmd_args"] for c in link["cmds"] if "$(S)/build/scripts/link_dyn_lib.py" in c["cmd_args"])
        self.assertLess(args.index("$(B)/extensions/comet/first.cpp.pic.o"),
                        args.index("$(B)/extensions/comet/second.cpp.pic.o"))
        self.assertLess(args.index("-Wl,--first-peer"), args.index("-Wl,--second-peer"))


if __name__ == "__main__":
    unittest.main(verbosity=2)
