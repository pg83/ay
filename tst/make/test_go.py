from tst.make.lib import MakeTest, PROJECT


class GoTest(MakeTest):
    def test_program_metadata(self):
        self.write("ya.make", "GO_PROGRAM(answer)\nLICENSE(MIT)\nVERSION(1.2.3)\nUSE_SDC_META()\nSRCS(main.go)\nEND()\n")
        self.write("main.go", "package main\nfunc main() {}\n")
        graph = self.graph("--build=release")
        component = self.node(graph, "answer.GO.component.sbom")
        program = self.node(graph, "answer")
        self.assertIn(component["uid"], program["deps"])
        self.assertIn(f"$(B)/{PROJECT}/answer.GO.component.sbom", program["inputs"])
        commands = [command["cmd_args"] for command in program["cmds"]]
        merge = next(i for i, args in enumerate(commands) if any(arg.endswith("/merge_sdcmeta.py") for arg in args))
        embed = next(i for i, args in enumerate(commands) if any(arg.startswith(".rosbomdata=") for arg in args))
        self.assertLess(merge, embed)

    def test_go_library_sources(self):
        self.write("ya.make", "GO_LIBRARY()\nSRCS(answer.go)\nEND()\n")
        self.write("answer.go", "package answer\nfunc Value() int { return 42 }\n")
        graph = self.graph()
        compilation = [node for node in graph["graph"] if f"$(S)/{PROJECT}/answer.go" in node["inputs"]]
        self.assertTrue(compilation, "no command consumes the Go source")
        self.assertTrue(any(path.endswith(".a") for node in compilation for path in node["outputs"]))
