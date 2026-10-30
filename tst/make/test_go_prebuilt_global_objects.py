from tst.make.lib import MakeTest


class GoPrebuiltGlobalObjectsTest(MakeTest):
    def test_prebuilt_global_objects_are_inputs_to_the_external_go_linker(self):
        self.write("ya.make", "GO_PROGRAM(answer)\nCGO_SRCS(main.go)\nGLOBAL_SRCS(prebuilt.o)\nEND()\n")
        self.write("main.go", 'package main\nimport "C"\nfunc main() {}\n')
        (self.project / "prebuilt.o").write_bytes(b"\x7fELF")
        node = self.node(self.graph(), "answer")
        self.assertIn(f"$(S)/{self.project_name}/prebuilt.o", node["inputs"])
        args = [arg for command in node["cmds"] for arg in command["cmd_args"]]
        self.assertTrue(any("prebuilt.o" in arg for arg in args), args)
