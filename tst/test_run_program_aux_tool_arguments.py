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


if __name__ == "__main__":
    unittest.main()
