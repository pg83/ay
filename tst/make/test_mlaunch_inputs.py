from tst.make.lib import MakeTest, PROJECT


class MlaunchInputsTest(MakeTest):
    def test_go_component_resolves_its_source_directory(self):
        self.write("ya.make", f"GO_PROGRAM(answer)\nSRCS(main.go)\nPEERDIR({PROJECT}/component)\nEND()\n")
        self.write("main.go", "package main\nfunc main() {}\n")
        self.write("component/ya.make", f"MLAUNCH_COMPONENT(component)\nONLY_TAGS(GO_MLAUNCH)\nGOLAUNCH_SRCDIR({PROJECT}/sources)\nGOLAUNCH_SRCS(component.go)\nEND()\n")
        self.write("sources/component.go", "package component\nfunc Launch() {}\n")
        graph = self.graph()
        archive = self.node(graph, "component/component.a")
        self.assertIn(f"$(S)/{PROJECT}/sources/component.go", archive["inputs"])
        self.assertIn(archive["uid"], self.node(graph, "answer")["deps"])

    def test_go_component_validates_node_builder_options(self):
        self.write("ya.make", f"GO_PROGRAM(answer)\nSRCS(main.go)\nPEERDIR({PROJECT}/component)\nEND()\n")
        self.write("main.go", "package main\nfunc main() {}\n")
        self.write("component/component.go", "package component\nfunc Launch() {}\n")
        options = (
            "Build NAME", "Build UNKNOWN value",
            "Build NAME first NAME second TARGET app HEADER value.h",
            "Build NAME first",
        )
        for args in options:
            with self.subTest(args=args):
                self.write("component/ya.make", f"MLAUNCH_COMPONENT(component)\nONLY_TAGS(GO_MLAUNCH)\nGOLAUNCH_SRCS(component.go)\nNODE_BUILDER({args})\nEND()\n")
                result = self.make()
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("NODE_BUILDER", result.stderr)

    def test_golaunch_options_do_not_generate_sources_in_plain_go_libraries(self):
        self.write("ya.make", "GO_LIBRARY()\nGOLAUNCH_FUNCTION(Launch)\nGOLAUNCH_OVERRIDE(target Launch)\nGENERATE_GOLAUNCH_CALLBACKS(callback)\nSRCS(answer.go)\nEND()\n")
        self.write("answer.go", "package answer\nfunc Launch() {}\n")
        graph = self.graph()
        archive = self.node(graph, f"{PROJECT}.a")
        self.assertIn(f"$(S)/{PROJECT}/answer.go", archive["inputs"])
        self.assertFalse(any(f"$(B)/{PROJECT}/launch.go" in node["outputs"] for node in graph["graph"]))


class MlaunchPipelineTest(MakeTest):
    project_name = "sdg/sdc/ros/monitorings/diag_updater/__ay_e2e__"

    def test_go_pipeline_generates_launch_and_metadata(self):
        self.write("ya.make", "MLAUNCH_PIPELINE(answer)\nONLY_TAGS(GOLAUNCH)\nEND()\n")
        graph = self.graph()
        generated = self.node(graph, "launch.go")
        binary = self.node(graph, "answer.golaunch")
        self.assertIn(generated["uid"], binary["deps"])
        self.assertIn(f"$(B)/{self.project_name}/answer.golaunch.sdcmeta", binary["outputs"])

    def test_cpp_pipeline_generates_metadata(self):
        self.write("ya.make", "MLAUNCH_PIPELINE(answer)\nONLY_TAGS(PIPELINE)\nEND()\n")
        binary = self.node(self.graph(), "answer")
        self.assertIn(f"$(B)/{self.project_name}/answer.sdcmeta", binary["outputs"])

    def test_deployment_contains_both_pipeline_binaries(self):
        self.write("ya.make", "MLAUNCH_PIPELINE(answer)\nEND()\n")
        graph = self.graph()
        deployment = self.node(graph, "answer.final.pkg.fake")
        for output in ("answer", "answer.golaunch"):
            binary = self.node(graph, output)
            self.assertIn(binary["uid"], deployment["deps"])
            self.assertIn(f"$(B)/{self.project_name}/{output}", deployment["inputs"])
