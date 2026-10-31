from tst.make.lib import MakeTest, PROJECT


class GeneratedCudaOwnershipTest(MakeTest):
    def test_generated_cuda_sources_keep_their_source_dependencies_with_ownership_checks(self):
        self.write("ya.make", "LIBRARY(answer)\nCOPY_FILE(template.cu generated.cu)\nSRCS(generated.cu)\nEND()\n")
        self.write("template.cu", "__global__ void answer(int* value) { *value = 42; }\n")
        graph = self.graph(env={"AY_DEBUG_OWNERSHIP": "1"})
        generated = self.node(graph, "generated.cu")
        compiled = self.node(graph, "generated.cu.o")
        self.assertIn(f"$(S)/{PROJECT}/template.cu", generated["inputs"])
        self.assertIn(f"$(B)/{PROJECT}/generated.cu", compiled["inputs"])
        self.assertIn(generated["uid"], compiled["deps"])
