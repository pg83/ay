import unittest

from tst import lib


def workspace(body):
    files = {"catalog/packed/ya.make": f"UNION()\n{body}\nEND()\n"}
    lib.tool_program(files, "tools/pack-index", "indexer")
    return files


class RunProgramCallArgumentsTest(unittest.TestCase):
    def test_tool_list_expands_before_binding_positional_arguments(self):
        files = workspace(
            "SET(PACK_INDEX tools/pack-index -A ${ARCADIA_ROOT})\n"
            "SET(RESULT ${ARCADIA_BUILD_ROOT}/catalog/packed)\n"
            "RUN_PROGRAM(${PACK_INDEX} ${CURDIR}/records.list ${RESULT}/records.bin "
            "IN ${CURDIR}/records.list OUT_NOAUTO ${RESULT}/records.bin)"
        )
        files["catalog/packed/records.list"] = "record\n"
        graph = lib.make(files, "catalog/packed")
        node = lib.node_by_output(graph, "$(B)/catalog/packed/records.bin")
        self.assertEqual(node["cmds"][0]["cmd_args"], [
            "$(B)/tools/pack-index/indexer", "-A", "$(S)",
            "$(S)/catalog/packed/records.list", "$(B)/catalog/packed/records.bin",
        ])
        self.assertCountEqual(node["inputs"], [
            "$(B)/tools/pack-index/indexer", "$(S)/catalog/packed/records.list",
        ])
        self.assertIn(lib.node_by_output(graph, "$(B)/tools/pack-index/indexer")["uid"], node["deps"])

    def test_expanded_lists_bind_keywords_inputs_outputs_tools_env_and_cwd(self):
        files = workspace(
            "SET(PACK_INDEX ${ARCADIA_ROOT}/tools/pack-index --mode compact)\n"
            "SET(CALL ${PACK_INDEX} IN seed.list IN_NOPARSE opaque.cpp "
            "OUT_NOAUTO records.bin TOOL ${ARCADIA_ROOT}/tools/stamp "
            "ENV FORMAT=packed CWD ${ARCADIA_ROOT})\n"
            "RUN_PROGRAM(${CALL})"
        )
        files["catalog/packed/seed.list"] = "seed\n"
        files["catalog/packed/opaque.cpp"] = '#include "must-not-parse.h"\n'
        lib.tool_program(files, "tools/stamp", "stamp")
        graph = lib.make(files, "catalog/packed")
        node = lib.node_by_output(graph, "$(B)/catalog/packed/records.bin")
        command = node["cmds"][0]
        self.assertEqual(command["cmd_args"], ["$(B)/tools/pack-index/indexer", "--mode", "compact"])
        self.assertEqual(command["cwd"], "$(S)")
        self.assertEqual(command["env"]["FORMAT"], "packed")
        self.assertCountEqual(node["inputs"], [
            "$(B)/tools/pack-index/indexer", "$(B)/tools/stamp/stamp",
            "$(S)/catalog/packed/seed.list", "$(S)/catalog/packed/opaque.cpp",
        ])
        for output in ("tools/pack-index/indexer", "tools/stamp/stamp"):
            self.assertIn(lib.node_by_output(graph, "$(B)/" + output)["uid"], node["deps"])

    def test_optional_first_argument_and_expanded_sections(self):
        files = workspace(
            "SET(OUTPUTS OUT_NOAUTO records.bin)\n"
            "RUN_PROGRAM(${UNSET_TOOL_PREFIX} tools/pack-index --mode compact ${OUTPUTS})"
        )
        graph = lib.make(files, "catalog/packed")
        node = lib.node_by_output(graph, "$(B)/catalog/packed/records.bin")
        self.assertEqual(node["cmds"][0]["cmd_args"], ["$(B)/tools/pack-index/indexer", "--mode", "compact"])

    def test_empty_expanded_call_reports_missing_tool(self):
        result = lib.make_process(workspace("RUN_PROGRAM(${UNSET_CALL})"), "catalog/packed")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("RUN_PROGRAM expects at least 1 argument (tool path)", result.stderr)
        self.assertNotIn("panic:", result.stderr)

    def test_missing_expanded_tool_keeps_configuration_error(self):
        files = workspace("SET(CALL tools/missing --mode packed)\nRUN_PROGRAM(${CALL} OUT_NOAUTO records.bin)")
        result = lib.make_process(files, "catalog/packed")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("tools/missing/ya.make: no such file or directory", result.stderr)
        self.assertNotIn("$(S)/", result.stderr)

    def test_cached_lexical_call_keeps_each_modules_variable_context(self):
        files = workspace("")
        files.update({
            "catalog/packed/ya.make": "UNION()\nSET(CALL tools/pack-index --mode packed)\nINCLUDE(${ARCADIA_ROOT}/shared/generate.inc)\nEND()\n",
            "catalog/plain/ya.make": "UNION()\nSET(CALL tools/pack-index --mode plain)\nINCLUDE(${ARCADIA_ROOT}/shared/generate.inc)\nEND()\n",
            "shared/generate.inc": "RUN_PROGRAM(${CALL} --source ${CURDIR} --dest ${BINDIR} OUT_NOAUTO records.bin)\n",
        })
        for targets in (("catalog/packed", "catalog/plain"), ("catalog/plain", "catalog/packed")):
            with self.subTest(targets=targets):
                graph = lib.make(files, *targets)
                for mode in ("packed", "plain"):
                    node = lib.node_by_output(graph, f"$(B)/catalog/{mode}/records.bin")
                    self.assertEqual(node["cmds"][0]["cmd_args"], [
                        "$(B)/tools/pack-index/indexer", "--mode", mode,
                        "--source", f"$(S)/catalog/{mode}", "--dest", f"$(B)/catalog/{mode}",
                    ])

    def test_source_root_cwd_and_recursed_test_dependency_resolve_independently(self):
        files = workspace(
            "RUN_PROGRAM(tools/pack-index --dest-directory ${BINDIR} "
            "CWD ${ARCADIA_ROOT} OUT_NOAUTO ${BINDIR}/records.bin)"
        )
        files["catalog/packed/ya.make"] += "RECURSE_FOR_TESTS(checks)\n"
        files["catalog/packed/checks/ya.make"] = (
            "EXECTEST()\nRUN(true)\n"
            "DEPENDS(${ARCADIA_ROOT}/catalog/packed catalog/packed)\nEND()\n"
        )
        graph = lib.make(files, "catalog/packed", "-t")
        node = lib.node_by_output(graph, "$(B)/catalog/packed/records.bin")
        self.assertEqual(node["cmds"][0]["cwd"], "$(S)")
        self.assertEqual(node["cmds"][0]["cmd_args"], [
            "$(B)/tools/pack-index/indexer", "--dest-directory", "$(B)/catalog/packed",
        ])
        suite = lib.node_by_output(graph, "$(B)/catalog/packed/checks/test-results/exectest/meta.json")
        union = lib.node_by_output(graph, "$(B)/catalog/packed/catalog-packed.pkg.fake")
        self.assertIn(node["uid"], union["deps"])
        self.assertIn(union["uid"], suite["deps"])
        self.assertEqual(suite["deps"].count(union["uid"]), 1)

    def test_missing_rooted_project_dependency_keeps_configuration_error(self):
        files = {"checks/ya.make": "EXECTEST()\nRUN(true)\nDEPENDS(${ARCADIA_ROOT}/missing/project)\nEND()\n"}
        result = lib.make_process(files, "checks", "-t")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("missing/project/ya.make: no such file or directory", result.stderr)
        self.assertNotIn("$(S)/", result.stderr)


if __name__ == "__main__":
    unittest.main(verbosity=2)
