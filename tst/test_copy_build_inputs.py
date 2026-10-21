import unittest

import lib


def files(source="generated.txt"):
    return {
        "pkg/ya.make": "UNION()\nSRCDIR(inputs)\nCOPY_FILE(seed.txt generated.txt)\n"
            f"COPY_FILE({source} installed.txt)\nEND()\n",
        "inputs/seed.txt": "seed\n",
    }


class CopyBuildInputTest(unittest.TestCase):
    def test_copy_reads_an_existing_generated_input_after_source_search(self):
        graph = lib.make(files(), "pkg")
        producer = lib.node_by_output(graph, "$(B)/pkg/generated.txt")
        installed = lib.node_by_output(graph, "$(B)/pkg/installed.txt")
        self.assertEqual(installed["inputs"], ["$(B)/pkg/generated.txt"])
        self.assertEqual(installed["cmds"][0]["cmd_args"][-2:], ["$(B)/pkg/generated.txt", "$(B)/pkg/installed.txt"])
        self.assertIn(producer["uid"], installed["deps"])

    def test_existing_source_file_precedes_the_build_candidate(self):
        for directory in ("pkg", "inputs"):
            with self.subTest(directory=directory):
                sources = files()
                sources[directory + "/generated.txt"] = "physical source\n"
                graph = lib.make(sources, "pkg")
                producer = lib.node_by_output(graph, "$(B)/pkg/generated.txt")
                installed = lib.node_by_output(graph, "$(B)/pkg/installed.txt")
                self.assertEqual(installed["inputs"], ["$(S)/" + directory + "/generated.txt"])
                self.assertNotIn(producer["uid"], installed["deps"])

    def test_explicit_source_keeps_its_root_when_a_build_candidate_exists(self):
        sources = files("${ARCADIA_ROOT}/pkg/generated.txt")
        sources["pkg/generated.txt"] = "physical source\n"
        graph = lib.make(sources, "pkg")
        producer = lib.node_by_output(graph, "$(B)/pkg/generated.txt")
        installed = lib.node_by_output(graph, "$(B)/pkg/installed.txt")
        self.assertEqual(installed["inputs"], ["$(S)/pkg/generated.txt"])
        self.assertNotIn(producer["uid"], installed["deps"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
