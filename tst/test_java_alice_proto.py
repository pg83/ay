import unittest

import lib
from test_go_tests import go_world
from test_java import contrib
from test_java_features import tree


PLUGIN = "alice/library/go/java_marker_interface_protoc_plugin"
DEPENDENCIES = {
    "ALICE_CAPABILITY": ("alice_capability_java", (
        "alice/protos/endpoint/java_interface", "alice/megamind/protos/scenarios/java_interface",
    )),
    "ALICE_TYPED_CALLBACK": ("alice_typed_callback_java", (
        "alice/protos/api/typed_callbacks/java_marker_interface",
    )),
}


def files(macro, selected=True):
    sources = go_world()
    sources.update(tree("PEERDIR(proto/api)", module="JAVA_LIBRARY_NON_SPLIT"))
    sources.update(contrib("contrib/java/com/google/protobuf/protobuf-java/4.33.0", jar=42))
    sources["proto/api/ya.make"] = f"PROTO_LIBRARY()\nONLY_TAGS({'JAVA_PROTO' if selected else 'DESC_PROTO'})\n{macro}()\nSRCS(api.proto)\nEND()\n"
    sources["proto/api/api.proto"] = 'syntax = "proto3";\n'
    for _, dependencies in DEPENDENCIES.values():
        for dependency in dependencies:
            sources[dependency + "/ya.make"] = "JAVA_LIBRARY(markers)\nJAVA_SRCS(Marker.java)\nEND()\n"
            sources[dependency + "/Marker.java"] = "class Marker {}\n"
    sources[PLUGIN + "/ya.make"] = f"GO_PROGRAM()\n{lib.BARE_MODULE}SRCS(main.go)\nEND()\n"
    sources[PLUGIN + "/main.go"] = "package main\nfunc main() {}\n"
    return sources


class AliceJavaProtoTest(unittest.TestCase):
    def test_alice_macros_keep_java_plugin_and_interface_dependencies(self):
        for macro, (name, dependencies) in DEPENDENCIES.items():
            for targets in (("app",), ("proto/api", "app"), ("app", "proto/api")):
                with self.subTest(macro=macro, targets=targets):
                    graph = lib.make(files(macro), *targets)
                    generated = lib.node_by_output(graph, "$(B)/proto/api/api.jsrc")
                    plugin_output = "$(B)/" + PLUGIN + "/java_marker_interface_protoc_plugin"
                    args = generated["cmds"][0]["cmd_args"]
                    self.assertIn(f"--plugin=protoc-gen-{name}={plugin_output}", args)
                    self.assertIn(f"--{name}_out=$(B)/java_out", args)
                    plugin = lib.node_by_output(graph, plugin_output)
                    self.assertIn(plugin_output, generated["inputs"])
                    self.assertIn(plugin["uid"], generated["deps"])
                    app = lib.node_by_output(graph, "$(B)/app/app.jar")
                    for dependency in dependencies:
                        interface = lib.node_by_output(graph, f"$(B)/{dependency}/markers-interface.jar")
                        self.assertIn(interface["outputs"][0], app["inputs"])
                        self.assertIn(interface["uid"], app["deps"])

    def test_excluded_java_variant_does_not_demand_java_plugin_or_interfaces(self):
        for macro in DEPENDENCIES:
            with self.subTest(macro=macro):
                graph = lib.make(files(macro, selected=False), "app")
                for node in graph["graph"]:
                    self.assertFalse(any(PLUGIN in output or output.endswith("markers-interface.jar") for output in node["outputs"]))


if __name__ == "__main__":
    unittest.main(verbosity=2)
