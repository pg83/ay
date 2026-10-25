import unittest

from tst import lib


def fixture(source="grower/created.inc", statements=""):
    files = {
        "grower/ya.make": f"LIBRARY()\n{lib.BARE_MODULE}"
            "RUN_PROGRAM(tools/seed IN seed.txt OUT_NOAUTO anchor.dat created.inc)\nEND()\n",
        "grower/seed.txt": "generated peer payload\n",
        "reader/ya.make": "UNION()\nPEERDIR(grower)\n" + statements
            + f"COPY_FILE({source} copied.inc)\nEND()\n",
    }
    lib.tool_program(files, "tools/seed", "renamed-seed")
    return files


class CopyPeerBuildInputsTest(unittest.TestCase):
    def test_root_relative_peer_output_resolves_its_registered_producer(self):
        for source in ("grower/created.inc", "./grower/created.inc",
                       "${ARCADIA_BUILD_ROOT}/grower/created.inc"):
            with self.subTest(source=source):
                graph = lib.make(fixture(source), "reader")
                generated = lib.node_by_output(graph, "$(B)/grower/created.inc")
                copied = lib.node_by_output(graph, "$(B)/reader/copied.inc")
                self.assertEqual(copied["cmds"][0]["cmd_args"][-2:], [
                    "$(B)/grower/created.inc", "$(B)/reader/copied.inc",
                ])
                self.assertIn("$(B)/grower/created.inc", copied["inputs"])
                self.assertIn("$(B)/grower/anchor.dat", copied["inputs"])
                self.assertIn(generated["uid"], copied["deps"])
                self.assertIn("$(S)/grower/seed.txt", generated["inputs"])
                tool = lib.node_by_output(graph, "$(B)/tools/seed/renamed-seed")
                self.assertIn(tool["outputs"][0], generated["inputs"])
                self.assertIn(tool["uid"], generated["deps"])

    def test_existing_sources_precede_the_peer_build_candidate(self):
        for source, physical in (("grower/created.inc", "grower/created.inc"),
                                 ("grower/created.inc", "reader/grower/created.inc"),
                                 ("${ARCADIA_ROOT}/grower/created.inc", "grower/created.inc")):
            with self.subTest(source=source, physical=physical):
                files = fixture(source)
                files[physical] = "physical source wins\n"
                graph = lib.make(files, "reader")
                copied = lib.node_by_output(graph, "$(B)/reader/copied.inc")
                self.assertEqual(copied["inputs"], ["$(S)/" + physical])
                self.assertEqual(copied["cmds"][0]["cmd_args"][-2:], [
                    "$(S)/" + physical, "$(B)/reader/copied.inc",
                ])
                self.assertEqual(copied["deps"], [])

    def test_local_registered_producer_precedes_the_peer_build_candidate(self):
        files = fixture(statements="COPY_FILE(local.txt grower/created.inc)\n")
        files["reader/local.txt"] = "local generated payload\n"
        graph = lib.make(files, "reader")
        local = lib.node_by_output(graph, "$(B)/reader/grower/created.inc")
        copied = lib.node_by_output(graph, "$(B)/reader/copied.inc")
        self.assertEqual(copied["inputs"], ["$(B)/reader/grower/created.inc"])
        self.assertEqual(copied["deps"], [local["uid"]])


if __name__ == "__main__":
    unittest.main(verbosity=2)
