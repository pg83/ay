from tst.make.lib import MakeTest, PROJECT


class PrebuiltPythonToolchainTest(MakeTest):
    def test_prebuilt_program_adds_its_required_python_toolchain(self):
        self.write_tree("build/platform/python/ymake_python3/ya.make", "RESOURCES_LIBRARY()\nNO_YMAKE_PYTHON3()\nEND()\n")
        self.write("ya.make", "YQL_PREBUILT_PROGRAM(answer)\nNO_YMAKE_PYTHON3()\nSET(ANSWER_RESOURCE_DEFAULT 123456789)\nSRCS(main.cpp)\nEND()\n")
        self.write("main.cpp", "int main() { return 0; }\n")
        graph = self.graph("-DBUILD_PYTHON3_BIN=python3")
        downloaded = self.node(graph, "answer.prebuilt")
        self.assertTrue(any("123456789" in arg for command in downloaded["cmds"] for arg in command["cmd_args"]))
        linked = self.node(graph, "answer")
        self.assertIn(downloaded["uid"], linked["deps"])
        self.assertIn(f"$(B)/{PROJECT}/answer.prebuilt", linked["inputs"])
