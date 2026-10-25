import unittest

from tst import lib


def files(optional=False):
    sources = {
        "p/ya.make": "UNION()\nSET(EMPTY)\n"
            + ("SET(OPTIONAL_ARGS --extra enabled)\nSET(OPTIONAL_OUT extra.bin)\nSET(OPTIONAL_INPUTS payload/two)\n" if optional else "")
            + "SET_LIST_PRE_SUF(REAL_INPUTS ${ARCADIA_ROOT}/ /data.txt payload/one)\n"
            "SET_LIST_PRE_SUF(EXTRA_INPUTS ${ARCADIA_ROOT}/ /data.txt ${OPTIONAL_INPUTS})\n"
            "RUN_PROGRAM(tools/gen --before ${OPTIONAL_ARGS} --after ${EMPTY} --value=pre${UNDEFINED_EMBED}post "
            "IN_NOPARSE ${REAL_INPUTS} ${EXTRA_INPUTS} OUT_NOAUTO main.bin ${OPTIONAL_OUT})\nEND()\n",
        "payload/one/data.txt": "real input\n",
        "payload/two/data.txt": "optional input\n",
    }
    lib.tool_program(sources, "tools/gen", "gen")
    return sources


class ArgumentExpansionTest(unittest.TestCase):
    def test_defined_resource_and_deferred_module_roots_survive(self):
        sources = {
            "p/ya.make": "UNION()\nRUN_PROGRAM(tools/gen --build ${BINDIR} --source ${CURDIR} "
                "--resource=${LATE_RESOURCE_GLOBAL}/bin/tool --unset=$ABSENT "
                "--value=pre$ABSENT.post OUT_NOAUTO main.bin)\nEND()\n",
        }
        lib.tool_program(sources, "tools/gen", "gen")
        graph = lib.make(sources, "p", "-DLATE_RESOURCE_GLOBAL=$(B)/resources/Declared")
        node = lib.node_by_output(graph, "$(B)/p/main.bin")
        self.assertEqual(node["cmds"][0]["cmd_args"], [
            "$(B)/tools/gen/gen", "--build", "$(B)/p", "--source", "$(S)/p",
            "--resource=$(B)/resources/Declared/bin/tool", "--unset=", "--value=pre.post",
        ])

    def test_assignment_does_not_resolve_a_later_variable_for_commands(self):
        sources = {
            "p/ya.make": "UNION()\nSET(ITEM ${LATER})\nSET(LATER payload/one)\n"
                "SET_LIST_PRE_SUF(INPUTS ${ARCADIA_ROOT}/ /data.txt ${ITEM})\n"
                "RUN_PROGRAM(tools/gen ${ITEM} IN_NOPARSE ${INPUTS} OUT_NOAUTO main.bin)\nEND()\n",
            "payload/one/data.txt": "input\n",
        }
        lib.tool_program(sources, "tools/gen", "gen")
        graph = lib.make(sources, "p")
        node = lib.node_by_output(graph, "$(B)/p/main.bin")
        self.assertEqual(node["cmds"][0]["cmd_args"], ["$(B)/tools/gen/gen"])
        self.assertNotIn("$(S)/payload/one/data.txt", node["inputs"])

    def test_unset_optional_lists_do_not_create_arguments_outputs_or_inputs(self):
        graph = lib.make(files(), "p")
        node = lib.node_by_output(graph, "$(B)/p/main.bin")
        self.assertEqual(node["outputs"], ["$(B)/p/main.bin"])
        self.assertEqual(node["cmds"][0]["cmd_args"], ["$(B)/tools/gen/gen", "--before", "--after", "--value=prepost"])
        self.assertIn("$(S)/payload/one/data.txt", node["inputs"])
        self.assertNotIn("$(S)/payload/two/data.txt", node["inputs"])
        self.assertFalse(any("${" in path for path in node["inputs"]))

    def test_defined_optional_lists_and_rooted_prefixes_keep_real_inputs(self):
        graph = lib.make(files(optional=True), "p")
        node = lib.node_by_output(graph, "$(B)/p/main.bin")
        self.assertEqual(node["outputs"], ["$(B)/p/main.bin", "$(B)/p/extra.bin"])
        self.assertEqual(node["cmds"][0]["cmd_args"], ["$(B)/tools/gen/gen", "--before", "--extra", "enabled", "--after", "--value=prepost"])
        self.assertIn("$(S)/payload/one/data.txt", node["inputs"])
        self.assertIn("$(S)/payload/two/data.txt", node["inputs"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
