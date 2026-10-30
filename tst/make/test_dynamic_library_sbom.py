from tst.make.lib import MakeTest, PROJECT


class DynamicLibrarySbomTest(MakeTest):
    def test_licensed_dynamic_library_keeps_its_component_metadata(self):
        self.write("ya.make", "DLL(answer)\nLICENSE(MIT)\nVERSION(1 2 3)\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        graph = self.graph("--build=release")
        component = self.node(graph, "answer.CPP.component.sbom")
        library = self.node(graph, "libanswer.so")
        self.assertIn(f"$(B)/{PROJECT}/answer.CPP.component.sbom", library["inputs"])
        self.assertIn(component["uid"], library["deps"])
        args = [arg for command in component["cmds"] for arg in command["cmd_args"]]
        self.assertEqual(args[args.index("--ver") + 1], "1.2.3")
