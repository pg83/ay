import unittest

from tst import lib
from tst.make.test_java import contrib, proxy
from tst.make.test_proto_targets import target_files


class JavaStartTargetsTest(unittest.TestCase):
    def files(self):
        return lib.java_tree(**{
            "external/ya.make": "EXTERNAL_JAVA_LIBRARY(generated)\nFROM_SANDBOX(FILE 19 OUT generated-input.jar)\nEND()\n",
            "consumer/ya.make": "JAVA_LIBRARY_NON_SPLIT()\nPEERDIR(external)\nEND()\n",
        })

    def test_ordinary_external_root_exposes_its_jar_and_registered_producer(self):
        for targets in (("external",), ("external", "consumer"), ("consumer", "external")):
            with self.subTest(targets=targets):
                graph = lib.make(self.files(), *targets)
                jar = lib.node_by_output(graph, "$(B)/external/generated.jar")
                producer = lib.node_by_output(graph, "$(B)/external/generated-input.jar")
                self.assertEqual(jar["kv"]["p"], "JV")
                self.assertIn("$(B)/external/generated-sources.jar", jar["outputs"])
                self.assertIn(producer["uid"], jar["deps"])
                self.assertIn("$(B)/external/generated-input.jar", jar["inputs"])
                self.assertIn(jar["uid"], graph["result"])

    def test_external_peer_is_a_dependency_without_becoming_a_requested_result(self):
        graph = lib.make(self.files(), "consumer")
        consumer = lib.node_by_output(graph, "$(B)/consumer/consumer.jar")
        external = lib.node_by_output(graph, "$(B)/external/generated.jar")
        self.assertIn(external["uid"], consumer["deps"])
        self.assertIn(consumer["uid"], graph["result"])
        self.assertNotIn(external["uid"], graph["result"])

    def test_fake_ordinary_proxy_has_no_requested_artifact(self):
        directory = "contrib/java/example/library"
        files = lib.java_tree(**{
            **proxy(directory, directory + "/1"),
            **contrib(directory + "/1", jar=23),
        })
        graph = lib.make(files, directory)
        self.assertEqual(graph["result"], [])

    def test_multimodule_root_keeps_interface_auxiliary_out_of_results(self):
        files = lib.java_tree(**{
            "split/ya.make": "JAVA_LIBRARY()\nEND()\n",
            "consumer/ya.make": "JAVA_LIBRARY_NON_SPLIT()\nPEERDIR(split)\nEND()\n",
        })
        lib.tool_program(files, "contrib/tools/ijar", "ijar")
        graph = lib.make(files, "consumer", "split")
        full = lib.node_by_output(graph, "$(B)/split/split.jar")
        interface = lib.node_by_output(graph, "$(B)/split/split-interface.jar")
        self.assertIn(full["uid"], graph["result"])
        self.assertNotIn(interface["uid"], graph["result"])

    def test_builtin_proto_root_keeps_its_selected_native_variant(self):
        files = target_files()
        files["p/ya.make"] = "PROTO_LIBRARY()\nONLY_TAGS(CPP_PROTO)\nSRCS(a.proto)\nEND()\n"
        graph = lib.make(files, "p")
        archive = lib.node_by_output(graph, "$(B)/p/libp.a")
        self.assertIn(archive["uid"], graph["result"])
        self.assertFalse(any(output.startswith("$(B)/p/") and output.endswith(".jar")
                             for node in graph["graph"] for output in node["outputs"]))


if __name__ == "__main__":
    unittest.main(verbosity=2)
