from tst.make.lib import MakeTest, PROJECT


class NativeModuleNamesTest(MakeTest):
    def test_unnamed_dynamic_library_uses_its_directory_name(self):
        self.write("ya.make", "DLL()\nSRCS(answer.cpp)\nCFLAGS(-DE2E_NAME=${REALPRJNAME})\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        graph = self.graph()
        self.node(graph, f"lib{PROJECT}.so")
        args = [arg for command in self.node(graph, "answer.cpp.pic.o")["cmds"] for arg in command["cmd_args"]]
        self.assertIn(f"-DE2E_NAME={PROJECT}", args)

    def test_constructor_prefix_does_not_replace_the_project_name(self):
        self.write("ya.make", "LIBRARY(PREFIX custom_ answer)\nSRCS(answer.cpp)\nCFLAGS(-DE2E_NAME=${REALPRJNAME})\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        compiled = self.node(self.graph(), "answer.cpp.o")
        args = [arg for command in compiled["cmds"] for arg in command["cmd_args"]]
        self.assertIn("-DE2E_NAME=answer", args)
