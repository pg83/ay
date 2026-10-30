from tst.make.lib import MakeTest


class ExtensionlessCudaSourceTest(MakeTest):
    def test_explicit_cuda_sources_do_not_require_a_filename_extension(self):
        self.write("ya.make", "LIBRARY(answer)\nSET(CUDA_ARCHITECTURES sm_75)\nCUDA_SRCS(source)\nEND()\n")
        self.write("source", "__global__ void answer(int* value) { *value = 42; }\n")
        graph = self.graph()
        compiled = self.node(graph, "source.cudafe1.cpp.o")
        self.assertIn(compiled["uid"], self.node(graph, "libanswer.a")["deps"])
        self.assertTrue(any(f"$(S)/{self.project_name}/source" in node["inputs"] for node in graph["graph"]))
