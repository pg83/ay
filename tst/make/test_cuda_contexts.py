from tst.make.lib import MakeTest, PROJECT


class CudaContextsTest(MakeTest):
    def test_cuda_outputs_in_pic_library(self):
        self.write("ya.make", f"DLL(answer)\nPEERDIR({PROJECT}/cuda)\nEND()\n")
        self.write("cuda/ya.make", "CUDA_DEVICE_LINK_LIBRARY()\nSRCS(answer.cu)\nNVCC_DEVICE_LINK(answer.cu)\nEND()\n")
        self.write("cuda/answer.cu", "__global__ void answer(int* value) { *value = 42; }\n")
        graph = self.graph()
        self.node(graph, "cuda/answer.cu.o")
        self.node(graph, "cuda/devlink.o")
        self.assertFalse(any(path.endswith(".cu.pic.o") for node in graph["graph"] for path in node["outputs"]))
