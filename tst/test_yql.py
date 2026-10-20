import unittest

import lib


BARE = lib.BARE_MODULE


class YqlAbiTest(unittest.TestCase):
    def test_consumers_select_distinct_transitive_abi_variants(self):
        files = {
            "stable/ya.make": f"PROGRAM()\n{BARE}PEERDIR(a)\nEND()\n",
            "current/ya.make": f"PROGRAM()\n{BARE}YQL_CURRENT_ABI_VERSION()\nPEERDIR(a)\nEND()\n",
            "a/ya.make": f"YQL_LIBRARY()\n{BARE}SRCS(a.cpp)\nPEERDIR(b)\nEND()\n",
            "a/a.cpp": '#include "a.h"\n',
            "a/a.h": "",
            "b/ya.make": f"YQL_LIBRARY()\n{BARE}SRCS(b.cpp)\nEND()\n",
            "b/b.cpp": "",
        }
        graph = lib.make(files, "stable", "current")
        for variant in ("stable", "current"):
            for module in ("a", "b"):
                obj = lib.node_by_output(graph, f"$(B)/{module}/{module}.cpp.abi_{variant}.o")
                args = obj["cmds"][0]["cmd_args"]
                self.assertIn(f"-DUSE_{variant.upper()}_UDF_ABI_VERSION", args)
                self.assertNotIn(f"-DUSE_{'CURRENT' if variant == 'stable' else 'STABLE'}_UDF_ABI_VERSION", args)
                archive = lib.node_by_output(graph, f"$(B)/{module}/yql-abi-{variant}-{module}.a")
                self.assertIn(obj["uid"], archive["deps"])
                program = lib.node_by_output(graph, f"$(B)/{variant}/{variant}")
                self.assertIn(archive["outputs"][0], program["inputs"])
        stable = lib.node_by_output(graph, "$(B)/a/a.cpp.abi_stable.o")
        current = lib.node_by_output(graph, "$(B)/a/a.cpp.abi_current.o")
        self.assertEqual(stable["inputs"], current["inputs"])

    def test_abi_flag_is_appended_to_cxxflags(self):
        # yql_udf.conf: the YQL_LIBRARY variants SET_APPEND(CXXFLAGS
        # $YQL_ABI_FLAGS), after the configured CXXFLAGS; ydb
        # .abi_stable.o compiles end their C++ flags with -nostdinc++
        # -DCATBOOST_OPENSOURCE=yes -nostdinc++ -DUSE_STABLE_UDF_ABI_VERSION.
        files = {
            "p/ya.make": f"PROGRAM()\n{BARE}PEERDIR(a)\nEND()\n",
            "a/ya.make": f"YQL_LIBRARY()\n{BARE}CXXFLAGS(-DOWN)\nSRCS(a.cpp)\nEND()\n",
            "a/a.cpp": "",
        }
        args = lib.node_by_output(lib.make(files, "p"), "$(B)/a/a.cpp.abi_stable.o")["cmds"][0]["cmd_args"]
        flag = args.index("-DUSE_STABLE_UDF_ABI_VERSION")
        self.assertLess(args.index("-DOWN"), flag)
        self.assertNotIn("-nostdinc++", args[flag:])

    def test_abi_flag_closes_the_bitcode_cxxflags(self):
        # The LLVM_COMPILE_CXX of ydb yt_codec_bc.cpp.16.bc renders the same
        # CXXFLAGS order as the object compile.
        files = {
            "p/ya.make": f"PROGRAM()\n{BARE}PEERDIR(a)\nEND()\n",
            "a/ya.make": f"YQL_LIBRARY()\n{BARE}USE_LLVM_BC20()\nLLVM_BC(a.cpp NAME n)\nEND()\n",
            "a/a.cpp": "",
            "build/platform/clang/ya.make": (
                "RESOURCES_LIBRARY()\nDECLARE_EXTERNAL_RESOURCE(CLANG20 sbr:1)\nEND()\n"
            ),
            "build/platform/python/ymake_python3/ya.make": (
                "RESOURCES_LIBRARY()\nDECLARE_EXTERNAL_RESOURCE(YMAKE_PYTHON3 sbr:2)\nEND()\n"
            ),
        }
        lib.tool_program(files, "tools/rescompiler", "rescompiler")
        lib.tool_program(files, "tools/rescompressor", "rescompressor")
        graph = lib.make(files, "p", "--target-platform", "default-linux-x86_64")
        args = lib.node_by_output(graph, "$(B)/a/a.cpp.abi_stable.bc")["cmds"][0]["cmd_args"]
        self.assertNotIn("-nostdinc++", args[args.index("-DUSE_STABLE_UDF_ABI_VERSION"):])

    def test_direct_library_selects_stable_variant(self):
        graph = lib.make({
            "a/ya.make": f"YQL_LIBRARY()\n{BARE}SRCS(a.cpp)\nEND()\n",
            "a/a.cpp": "",
        }, "a")
        lib.node_by_output(graph, "$(B)/a/a.cpp.abi_stable.o")
        self.assertFalse(any("abi_current" in o for n in graph["graph"] for o in n["outputs"]))

    def test_prebuilt_program_falls_back_to_declared_sources(self):
        graph = lib.make({
            "app/ya.make": (
                f"YQL_PREBUILT_PROGRAM(named)\n{BARE}SET(NAMED_RESOURCE_DEFAULT 42)\n"
                "SRCS(main.cpp)\nPEERDIR(dep)\nEND()\n"
            ),
            "app/main.cpp": "",
            "dep/ya.make": f"LIBRARY()\n{BARE}SRCS(dep.cpp)\nEND()\n",
            "dep/dep.cpp": "",
        }, "app")
        lib.node_by_output(graph, "$(B)/app/named")
        lib.node_by_output(graph, "$(B)/app/main.cpp.o")
        lib.node_by_output(graph, "$(B)/dep/dep.cpp.o")

    def test_prebuilt_program_selects_sanitizer_resource_and_discards_source_peers(self):
        for sanitizer, resource in (("", "42"), ("address", "43")):
            with self.subTest(sanitizer=sanitizer):
                graph = lib.make({
                    "app/ya.make": (
                        "YQL_PREBUILT_PROGRAM(named)\n"
                        "SET(NAMED_RESOURCE_DEFAULT 42)\nSET(NAMED_RESOURCE_ASAN 43)\n"
                        "SRCS(missing.cpp)\nPEERDIR(missing/peer)\nEND()\n"
                    ),
                }, "app", f"-DSANITIZER_TYPE={sanitizer}", opensource=False)
                fetch = lib.node_by_output(graph, "$(B)/app/named.prebuilt")
                args = fetch["cmds"][0]["cmd_args"]
                self.assertEqual(args[args.index("--resource-id") + 1], resource)
                self.assertIn("--executable", args)
                program = lib.node_by_output(graph, "$(B)/app/named")
                self.assertIn(fetch["uid"], program["deps"])
                self.assertIn("$(B)/app/named.prebuilt", program["inputs"])
                self.assertEqual(program["cmds"][0]["cmd_args"][-3:], [
                    "copy", "$(B)/app/named.prebuilt", "$(B)/app/named",
                ])

    def test_stable_abi_macros_follow_source_configuration_revision(self):
        for modern in (False, True):
            files = {
                "a/ya.make": f"LIBRARY()\n{BARE}YQL_LAST_ABI_VERSION()\nYQL_ABI_VERSION(2 40 1)\nSRCS(a.cpp)\nEND()\n",
                "a/a.cpp": "",
            }
            if modern:
                files["build/conf/project_specific/yql_udf.conf"] = "macro YQL_STABLE_ABI_VERSION() {\n}\n"
            node = lib.node_by_output(lib.make(files, "a"), "$(B)/a/a.cpp.o")
            args = node["cmds"][0]["cmd_args"]
            self.assertIn("-DUSE_STABLE_UDF_ABI_VERSION" if modern else "-DUSE_CURRENT_UDF_ABI_VERSION", args)
            self.assertIn("-DMIN_UDF_ABI_VERSION_MINOR=40" if modern else "-DUDF_ABI_VERSION_MINOR=40", args)


if __name__ == "__main__":
    unittest.main(verbosity=2)
