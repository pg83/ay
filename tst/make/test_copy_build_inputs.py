import unittest

from tst import lib


def files(source="generated.txt"):
    return {
        "pkg/ya.make": "UNION()\nSRCDIR(inputs)\nCOPY_FILE(seed.txt generated.txt)\n"
            f"COPY_FILE({source} installed.txt)\nEND()\n",
        "inputs/seed.txt": "seed\n",
    }


def build_directory_fallback_files():
    return {
        "grove/parcel/ya.make": "UNION()\nSRCDIR(${ARCADIA_BUILD_ROOT}/grove/parcel/missing)\n"
            "COPY_FILE(seed.txt installed.txt)\nEND()\n",
        "grove/parcel/seed.txt": "physical seed\n",
        "build/scripts/fs_tools.py": "import process_command_files\n",
        "build/scripts/process_command_files.py": "# Graph-only script input fixture.\n",
    }


class CopyBuildInputTest(unittest.TestCase):
    def test_missing_build_source_directory_falls_back_to_physical_module_source(self):
        graph = lib.make(build_directory_fallback_files(), "grove/parcel")
        output = "$(B)/grove/parcel/installed.txt"
        copies = [n for n in graph["graph"] if n["kv"]["p"] == "CP" and output in n["outputs"]]
        self.assertEqual(len(copies), 1)
        installed = copies[0]
        self.assertEqual(installed["outputs"], [output])
        self.assertEqual(set(installed["inputs"]), {
            "$(S)/grove/parcel/seed.txt", "$(S)/build/scripts/fs_tools.py",
            "$(S)/build/scripts/process_command_files.py",
        })
        self.assertEqual(installed["cmds"][0]["cmd_args"][-3:], ["copy", "$(S)/grove/parcel/seed.txt", output])
        package = lib.node_by_output(graph, "$(B)/grove/parcel/grove-parcel.pkg.fake")
        self.assertIn(output, package["outputs"])
        self.assertIn(output, package["inputs"])
        self.assertIn(installed["uid"], package["deps"])
        self.assertIn(package["uid"], graph["result"])
        self.assertFalse(any(value.startswith("$(B)/grove/parcel/missing/")
                             for node in graph["graph"] for value in node["inputs"]))
        ids = {n["uid"] for n in graph["graph"]}
        self.assertLessEqual(set(graph["result"]), ids)
        self.assertTrue(all(set(n["deps"]) <= ids for n in graph["graph"]))

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
