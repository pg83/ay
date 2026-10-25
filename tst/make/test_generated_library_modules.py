import unittest

from tst import lib


def files():
    sources = {
        "gen/ya.make": "GEN_LIBRARY()\nPEERDIR(empty)\nRUN_PROGRAM(tools/gen OUT_NOAUTO generated.h generated.tar)\nEND()\n",
        "empty/ya.make": "GEN_LIBRARY()\nEND()\n",
        "peer/ya.make": f"LIBRARY()\n{lib.BARE_MODULE}PEERDIR(gen)\nSRCS(peer.cpp)\nEND()\n",
        "peer/peer.cpp": '#include "gen/generated.h"\n',
        "app/ya.make": f"PROGRAM()\n{lib.BARE_MODULE}SRCS(main.cpp)\nPEERDIR(peer empty)\nEND()\n",
        "app/main.cpp": "int main() {}\n",
    }
    lib.tool_program(sources, "tools/gen", "gen")
    return sources


class GeneratedLibraryModulesTest(unittest.TestCase):
    def test_generated_library_keeps_nonauto_producers_and_transitive_module_inputs(self):
        for targets in (("app",), ("gen", "app"), ("app", "gen")):
            with self.subTest(targets=targets):
                graph = lib.make(files(), *targets)
                generated = lib.node_by_output(graph, "$(B)/gen/generated.h")
                module = lib.node_by_output(graph, "$(B)/gen/gen.pkg.fake")
                empty = lib.node_by_output(graph, "$(B)/empty/empty.pkg.fake")
                app = lib.node_by_output(graph, "$(B)/app/app")
                for output in generated["outputs"]:
                    self.assertIn(output, module["inputs"])
                self.assertIn(generated["uid"], module["deps"])
                self.assertEqual(module["outputs"], ["$(B)/gen/gen.pkg.fake"])
                for item in (module, empty):
                    self.assertIn(item["outputs"][0], app["inputs"])
                    self.assertIn(item["uid"], app["deps"])
                    self.assertIn(item["outputs"][0][len("$(B)/"):], [arg for cmd in app["cmds"] for arg in cmd["cmd_args"]])
                    self.assertEqual(sum(item["outputs"][0] in node["outputs"] for node in graph["graph"]), 1)
                self.assertNotIn(empty["outputs"][0], module["inputs"])
                self.assertNotIn(empty["uid"], module["deps"])

    def test_shared_generated_library_ancestors_are_linked_once(self):
        sources = files()
        sources["gen/ya.make"] = sources["gen/ya.make"].replace("PEERDIR(empty)", "PEERDIR(left right empty)")
        for module in ("left", "right"):
            sources[module + "/ya.make"] = "GEN_LIBRARY()\nPEERDIR(empty)\nEND()\n"
        graph = lib.make(sources, "app")
        app = lib.node_by_output(graph, "$(B)/app/app")
        args = [arg for cmd in app["cmds"] for arg in cmd["cmd_args"]]
        for module in ("left", "right", "empty", "gen"):
            output = f"$(B)/{module}/{module}.pkg.fake"
            node = lib.node_by_output(graph, output)
            self.assertEqual(app["inputs"].count(output), 1)
            self.assertEqual(app["deps"].count(node["uid"]), 1)
            self.assertEqual(args.count(output[len("$(B)/"):]), 1)

    def test_go_consumers_keep_resource_modules_out_of_cgo_peer_arguments(self):
        from tst.make.test_go_tests import go_world
        sources = go_world()
        sources.update(files())
        sources["g/ya.make"] = f"GO_LIBRARY()\n{lib.BARE_MODULE}PEERDIR(gen)\nSRCS(g.go)\nEND()\n"
        sources["g/g.go"] = "package g\n"
        sources["app/ya.make"] = f"GO_PROGRAM()\n{lib.BARE_MODULE}PEERDIR(g gen)\nSRCS(main.go)\nEND()\n"
        sources["app/main.go"] = "package main\nfunc main() {}\n"
        graph = lib.make(sources, "app")
        for output in ("$(B)/g/g.a", "$(B)/app/app"):
            consumer = lib.node_by_output(graph, output)
            args = [arg for cmd in consumer["cmds"] for arg in cmd["cmd_args"]]
            for path in ("gen/gen.pkg.fake", "empty/empty.pkg.fake"):
                module = lib.node_by_output(graph, "$(B)/" + path)
                self.assertIn("$(B)/" + path, consumer["inputs"])
                self.assertIn(module["uid"], consumer["deps"])
                self.assertNotIn(path, args)

    def test_generated_library_uses_default_and_explicit_names(self):
        for name, declaration in (("scope-group-gen", ""), ("chosen", "chosen")):
            with self.subTest(name=name):
                directory = "parent/scope/group/gen"
                sources = {directory + "/ya.make": f"GEN_LIBRARY({declaration})\nEND()\n",
                           "app/ya.make": f"PROGRAM()\n{lib.BARE_MODULE}SRCS(main.cpp)\nPEERDIR({directory})\nEND()\n",
                           "app/main.cpp": "int main() {}\n"}
                graph = lib.make(sources, "app")
                module = lib.node_by_output(graph, f"$(B)/{directory}/{name}.pkg.fake")
                touch = "$(S)/build/scripts/touch.py"
                self.assertEqual(module["cmds"][0]["cmd_args"][-2:], [touch, module["outputs"][0]])
                self.assertEqual(module["inputs"], [touch])


if __name__ == "__main__":
    unittest.main(verbosity=2)
