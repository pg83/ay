from tst.make.lib import MakeTest, PROJECT


class GoResourceObjectsTest(MakeTest):
    def test_resource_library_objects_are_inputs_to_the_go_linker(self):
        self.write("ya.make", f"GO_PROGRAM(answer)\nSRCS(main.go)\nPEERDIR({PROJECT}/assets)\nEND()\n")
        self.write("main.go", "package main\nfunc main() {}\n")
        self.write("assets/ya.make", "RESOURCES_LIBRARY()\nRESOURCE(value.txt /answer)\nEND()\n")
        self.write("assets/value.txt", "42\n")
        graph = self.graph()
        resources = [node for node in graph["graph"] if f"$(S)/{PROJECT}/assets/value.txt" in node["inputs"] and any(output.endswith(".o") for output in node["outputs"])]
        self.assertEqual(len(resources), 1)
        resource = resources[0]
        output = next(path for path in resource["outputs"] if path.endswith(".o"))
        linked = self.node(graph, "answer")
        self.assertIn(output, linked["inputs"])
        self.assertIn(resource["uid"], linked["deps"])
        args = [arg for command in linked["cmds"] for arg in command["cmd_args"]]
        self.assertTrue(any(output.removeprefix("$(B)/") in arg for arg in args))
