import unittest

import lib
from test_codegen_macros import mn_files, X86_64


MODULE = "orchard/models"


def model_files(two=False):
    files = {key.replace("mod/", MODULE + "/", 1): value
             for key, value in mn_files().items()}
    files.pop(f"{MODULE}/use.cpp")
    files[f"{MODULE}/ya.make"] = (
        f"LIBRARY()\n{lib.BARE_MODULE}BUILD_MN(model.info Forecast)\n"
        + ("BUILD_MN(second.info Confidence)\n" if two else "") + "END()\n"
    )
    if two:
        files[f"{MODULE}/second.info"] = ""
    files["kernel/matrixnet/mn_sse.h"] = '#include "details.h"\n'
    files["kernel/matrixnet/details.h"] = ""
    return files


class BuildMnOnlyTest(unittest.TestCase):
    def check_models(self, graph, names):
        archive = lib.node_by_output(graph, f"$(B)/{MODULE}/liborchard-models.a")
        expected_objects = []
        for name, info in zip(names, ("model.info", "second.info")):
            source = f"$(B)/{MODULE}/mn.{name}.cpp"
            payload = f"$(B)/{MODULE}/MN_External_{name}.rodata"
            producer = lib.node_by_output(graph, source)
            archiver = lib.node_by_output(graph, "$(B)/tools/archiver/archiver")
            self.assertEqual(producer["outputs"], [source, payload])
            self.assertIn(f"$(S)/{MODULE}/{info}", producer["inputs"])
            self.assertIn(archiver["uid"], producer["deps"])
            self.assertIn(archiver["outputs"][0], producer["inputs"])
            compiled = lib.node_by_output(graph, source + ".o")
            rodata = lib.node_by_output(graph, payload + ".o")
            self.assertEqual(rodata["outputs"], [payload + ".asm", payload + ".o"])
            for node in (compiled, rodata):
                self.assertIn(producer["uid"], node["deps"])
                self.assertIn(source, node["inputs"])
                self.assertIn("$(S)/kernel/matrixnet/mn_sse.h", node["inputs"])
                self.assertIn("$(S)/kernel/matrixnet/details.h", node["inputs"])
                self.assertIn(node["uid"], archive["deps"])
                self.assertIn(node["outputs"][-1], archive["inputs"])
            self.assertIn(payload, rodata["inputs"])
            expected_objects += [source + ".o", payload + ".o"]
        self.assertEqual([p for p in archive["inputs"] if p.endswith(".o")], expected_objects)
        return archive

    def test_model_declaration_alone_builds_the_generated_objects_and_archive(self):
        graph = lib.make(model_files(), MODULE, *X86_64)
        archive = self.check_models(graph, ("Forecast",))
        self.assertIn(archive["uid"], graph["result"])

    def test_model_only_peer_supplies_each_model_to_the_link(self):
        files = model_files(two=True)
        files["reader/ya.make"] = f"PROGRAM()\n{lib.BARE_MODULE}PEERDIR({MODULE})\nEND()\n"
        graph = lib.make(files, "reader", *X86_64)
        archive = self.check_models(graph, ("Forecast", "Confidence"))
        linked = lib.node_by_output(graph, "$(B)/reader/reader")
        self.assertIn(archive["outputs"][0], linked["inputs"])
        self.assertIn(archive["uid"], linked["deps"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
