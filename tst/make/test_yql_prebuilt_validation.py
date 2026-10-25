import unittest

from tst import lib


def files():
    return {
        "root/ya.make": f"LIBRARY()\n{lib.BARE_MODULE}SRCS(main.cpp)\nEND()\nRECURSE_FOR_TESTS(../prebuilt)\n",
        "root/main.cpp": "int value;\n",
        "prebuilt/ya.make": "YQL_PREBUILT_PROGRAM(named)\nSET(NAMED_RESOURCE_DEFAULT 42)\nSRCS(missing.cpp)\nPEERDIR(missing/peer)\nEND()\n",
    }


class YqlPrebuiltValidationTest(unittest.TestCase):
    def test_selected_prebuilt_resource_keeps_its_validation_check(self):
        graph = lib.make(files(), "root", "-t", "--output-only-tests", opensource=False)
        suite = lib.node_by_output(graph, "$(B)/prebuilt/test-results/validate_resource/meta.json")
        args = [arg for cmd in suite["cmds"] for arg in cmd["cmd_args"]]
        self.assertIn("42", args)
        for output in ("$(B)/prebuilt/named.prebuilt", "$(B)/prebuilt/named"):
            artifact = lib.node_by_output(graph, output)
            self.assertNotIn(output, suite["inputs"])
            self.assertNotIn(artifact["uid"], suite["deps"])

    def test_strong_program_demand_preserves_prebuilt_input_and_producer(self):
        graph = lib.make(files(), "prebuilt", opensource=False)
        fetch = lib.node_by_output(graph, "$(B)/prebuilt/named.prebuilt")
        program = lib.node_by_output(graph, "$(B)/prebuilt/named")
        self.assertIn(fetch["outputs"][0], program["inputs"])
        self.assertIn(fetch["uid"], program["deps"])

    def test_disabled_validation_preserves_the_requested_program_build(self):
        graph = lib.make(files(), "root", "-t", "--output-only-tests", "-DVALIDATE_DATA=no", opensource=False)
        self.assertFalse(any("/test-results/validate_resource/" in output for node in graph["graph"] for output in node["outputs"]))
        fetch = lib.node_by_output(graph, "$(B)/prebuilt/named.prebuilt")
        program = lib.node_by_output(graph, "$(B)/prebuilt/named")
        self.assertIn(fetch["uid"], program["deps"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
