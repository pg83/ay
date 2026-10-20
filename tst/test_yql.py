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

    def test_udf_test_is_a_python3_pytest_program_with_the_yql_test_tools(self):
        files = {
            "udf/ya.make": f"PROGRAM()\n{BARE}SRCS(u.cpp)\nEND()\nRECURSE_FOR_TESTS(test)\n",
            "udf/u.cpp": "int main(){return 0;}\n",
            "udf/test/ya.make": (
                f"YQL_UDF_TEST()\n{BARE}NO_CHECK_IMPORTS()\nNO_IMPORT_TRACING()\nNO_PYTHON_INCLUDES()\n"
                "DISABLE(PYTHON_SQLITE3)\nALLOCATOR(FAKE)\nSIZE(MEDIUM)\nDEPENDS(udf)\nEND()\n"
            ),
        }
        for peer in ["library/python/pytest", "yql/essentials/tests/common/udf_test", "library/python/runtime_py3/main", "library/cpp/resource"]:
            files[f"{peer}/ya.make"] = f"LIBRARY()\n{BARE}SRCS(p.cpp)\nEND()\n"
            files[f"{peer}/p.cpp"] = ""
        tools = ["yql/essentials/tools/astdiff", "yql/tools/yqlrun", "yql/essentials/tools/udf_resolver"]
        for tool in ["tools/archiver", "tools/py3cc", "tools/py3cc/slow", "tools/rescompiler", "tools/rescompressor", *tools]:
            lib.tool_program(files, tool, tool.rsplit("/", 1)[-1])
        graph = lib.make(files, "udf", "-t")
        binary = lib.node_by_output(graph, "$(B)/udf/test/udf-test")
        self.assertIn("$(B)/library/python/pytest/liblibrary-python-pytest.a", binary["inputs"])
        self.assertIn("$(B)/yql/essentials/tests/common/udf_test/libtests-common-udf_test.a", binary["inputs"])
        self.assertIn("PY_MAIN=library.python.pytest.main:main", " ".join(a for n in graph["graph"] for c in n["cmds"] for a in c["cmd_args"]))
        run = lib.node_by_output(graph, "$(B)/udf/test/test-results/py3test/meta.json")
        self.assertIn(binary["uid"], run["deps"])
        dep_outputs = {o for n in graph["graph"] if n["uid"] in run["deps"] for o in n["outputs"]}
        for dep in [*tools, "udf"]:
            self.assertIn(f"$(B)/{dep}/{dep.rsplit('/', 1)[-1]}", dep_outputs)
        args = run["cmds"][0]["cmd_args"]
        self.assertEqual(args[args.index("--test-type") + 1], "py3test")
        self.assertEqual([args[i + 1] for i, a in enumerate(args) if a == "--env"], [
            "YQL_ASTDIFF_PATH=yql/essentials/tools/astdiff/astdiff",
            "YQL_CONFIG_DIR=yql/essentials/cfg/udf_test",
            "YQL_YQLRUN_PATH=yql/tools/yqlrun/yqlrun",
            "YQL_SQL2YQL_PATH=yql/essentials/tools/sql2yql/sql2yql",
            "YQL_UDFRESOLVER_PATH=yql/essentials/tools/udf_resolver/udf_resolver",
        ])
        self.assertIn("$(S)/yql/essentials/cfg/udf_test", [args[i + 1] for i, a in enumerate(args) if a == "--test-related-path"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
