import unittest

import lib


PRIMARY = "orchard/driver"
SHORT = "sprig/compiler"
LONG = "sprig/deep/formatter"
ARGS = [
    SHORT, "--plugin=" + SHORT,
    "${ARCADIA_BUILD_ROOT}/" + SHORT + "/forge",
    "--plugin=${ARCADIA_BUILD_ROOT}/" + SHORT + "/forge",
    "${ARCADIA_ROOT}/" + SHORT,
    "prefix/" + SHORT, "x" + SHORT, SHORT + "/suffix",
    SHORT + ".data", "_" + SHORT, "-" + SHORT,
    "k:" + SHORT, "7" + SHORT, SHORT + "7",
    SHORT + ":" + SHORT,
    SHORT + ":" + LONG,
    SHORT + "/nested", "--nested=" + SHORT + "/nested",
]
EXPECTED = [
    "$(B)/" + SHORT + "/forge", "--plugin=$(B)/" + SHORT + "/forge",
    "$(B)/" + SHORT + "/forge", "--plugin=$(B)/" + SHORT + "/forge",
    "$(S)/" + SHORT,
    "prefix/" + SHORT, "x" + SHORT, SHORT + "/suffix", SHORT + ".data", "_" + SHORT, "-" + SHORT,
    "k:$(B)/" + SHORT + "/forge", "7$(B)/" + SHORT + "/forge", "$(B)/" + SHORT + "/forge7",
    "$(B)/" + SHORT + "/forge:" + SHORT,
    SHORT + ":$(B)/" + LONG + "/stamp",
    "$(B)/" + SHORT + "/nested/nested", "--nested=$(B)/" + SHORT + "/nested/nested",
]


def fixture(rooted=False, python=False):
    sources = {}
    for path, name in ((PRIMARY, "driver"), (SHORT, "forge"), (LONG, "stamp"), (SHORT + "/nested", "nested")):
        lib.tool_program(sources, path, name)
    tools = [SHORT, LONG, SHORT + "/nested"]
    if rooted:
        tools = ["${ARCADIA_ROOT}/" + tool for tool in tools]
    sources["orchard/packed/ya.make"] = (
        "UNION()\n" + ("RUN_PYTHON3(generator.py" if python else "RUN_PROGRAM(" + PRIMARY) + " " + " ".join(ARGS)
        + " TOOL " + " ".join(tools) + " OUT_NOAUTO record.bin)\nEND()\n"
    )
    sources["orchard/packed/generator.py"] = "print(1)\n"
    return sources


class RunProgramAuxToolArgumentsTest(unittest.TestCase):
    def check(self, rooted=False, python=False):
        sources = fixture(rooted, python)
        graph = lib.make(sources, "orchard/packed", "--sandboxing")
        node = next(n for n in graph["graph"] if n["kv"].get("p") == ("PY" if python else "PR") and "$(B)/orchard/packed/record.bin" in n["outputs"])
        command = node["cmds"][0]["cmd_args"]
        expected = [arg.replace("${ARCADIA_BUILD_ROOT}", "$(B)").replace("${ARCADIA_ROOT}", "$(S)") for arg in ARGS] if rooted else EXPECTED
        if python:
            self.assertEqual(command[1:], ["$(S)/orchard/packed/generator.py", *expected])
        else:
            self.assertEqual(command, ["$(B)/" + PRIMARY + "/driver", *expected])
        outputs = ["$(B)/" + p + "/" + n for p, n in ((PRIMARY, "driver"), (SHORT, "forge"), (LONG, "stamp"), (SHORT + "/nested", "nested"))]
        if python:
            outputs.remove("$(B)/" + PRIMARY + "/driver")
        self.assertCountEqual(node["inputs"], outputs + (["$(S)/orchard/packed/generator.py"] if python else []))
        tools = {lib.node_by_output(graph, output)["uid"] for output in outputs}
        self.assertEqual(set(node["deps"]), tools)
        self.assertEqual(set(node["foreign_deps"]["tool"]), tools)
        ids = {n["uid"] for n in graph["graph"]}
        self.assertTrue(graph["result"])
        self.assertLessEqual(set(graph["result"]), ids)
        self.assertTrue(all(set(n["deps"]) <= ids for n in graph["graph"]))

    def test_relative_aux_tools_replace_one_bounded_occurrence(self):
        self.check()

    def test_rooted_tool_declarations_keep_original_arguments(self):
        self.check(rooted=True)

    def test_python_wrapper_uses_the_same_auxiliary_tool_rules(self):
        self.check(python=True)

    def test_python_wrapper_keeps_rooted_tool_arguments(self):
        self.check(rooted=True, python=True)

    def test_run_program_rejects_an_empty_main_tool(self):
        self.check_empty_tool("RUN_PROGRAM(tools/empty OUT_NOAUTO record.bin)")

    def test_run_program_rejects_an_empty_auxiliary_tool(self):
        self.check_empty_tool("RUN_PROGRAM(tools/driver TOOL tools/empty OUT_NOAUTO record.bin)")

    def test_python_wrapper_rejects_an_empty_auxiliary_tool(self):
        self.check_empty_tool("RUN_PYTHON3(generator.py TOOL tools/empty OUT_NOAUTO record.bin)")

    def check_empty_tool(self, command):
        files = {
            "packed/ya.make": f"UNION()\n{command}\nEND()\n",
            "packed/generator.py": "print(1)\n",
            "tools/empty/ya.make": "LIBRARY()\n" + lib.BARE_MODULE + "END()\n",
            "tools/driver/ya.make": "PROGRAM(driver)\nALLOCATOR(FAKE)\n" + lib.BARE_MODULE + "SRCS(main.cpp)\nEND()\n",
            "tools/driver/main.cpp": "int main(){return 0;}\n",
        }
        result = lib.make_process(files, "packed", "--sandboxing")
        self.assertEqual(result.returncode, 1)
        self.assertIn("tool tools/empty has no linkable output", result.stderr)


if __name__ == "__main__":
    unittest.main()
