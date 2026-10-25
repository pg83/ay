import unittest

import lib
import test_resource_decls as resources


class PrebuiltPrimaryOutputTest(unittest.TestCase):
    def test_copy_keeps_literal_and_rooted_primary_output_arguments(self):
        for primary, argument in (
            ("relative/gen", "relative/gen"),
            ("/opt/bin/gen", "/opt/bin/gen"),
            ("${ARCADIA_ROOT}/source/gen", "$(S)/source/gen"),
            ("${BINDIR}/gen.bin", "$(B)/build/prebuilt/tools/gen/gen.bin"),
        ):
            with self.subTest(primary=primary):
                files = resources.PrebuiltProgramTest().prebuilt_files(
                    f"PREBUILT_PROGRAM()\nPRIMARY_OUTPUT({primary})\nEND()\n"
                )
                graph = lib.make(files, "lib")
                tool = lib.node_by_output(graph, "$(B)/build/prebuilt/tools/gen/gen")
                self.assertEqual(tool["cmds"][0]["cmd_args"][2:], [
                    "copy", argument, "$(B)/build/prebuilt/tools/gen/gen",
                ])
                self.assertNotIn(argument, tool["inputs"])
                self.assertEqual(tool["deps"], [])

    def test_unused_tool_with_empty_primary_expansion_is_not_published(self):
        for body in (
            "DECLARE_EXTERNAL_RESOURCE(GEN sbr:5)\n",
            "PRIMARY_OUTPUT(${GEN_RESOURCE_GLOBAL}/gen)\n",
            "DECLARE_EXTERNAL_RESOURCE(GEN sbr:5)\n"
            "PRIMARY_OUTPUT(${UNKNOWN_RESOURCE_GLOBAL}/gen)\n",
        ):
            with self.subTest(body=body):
                files = resources.PrebuiltProgramTest().prebuilt_files(f"PREBUILT_PROGRAM()\n{body}END()\n")
                files["lib/ya.make"] = files["lib/ya.make"].replace(
                    "RESOURCE(${BINDIR}/out.txt key)\n", "",
                )
                graph = lib.make(files, "lib")
                self.assertFalse(any(
                    output.startswith("$(B)/build/prebuilt/tools/gen/")
                    for node in graph["graph"] for output in node["outputs"]
                ))
                self.assertNotIn("$(B)/resources/GEN", resources.fetches(graph))


if __name__ == "__main__":
    unittest.main(verbosity=2)
