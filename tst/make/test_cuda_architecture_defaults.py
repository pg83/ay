from tst.make.lib import MakeTest


class CudaArchitectureDefaultsTest(MakeTest):
    def test_empty_architecture_list_falls_back_to_sm50(self):
        self.write("ya.make", "LIBRARY()\nSET(CUDA_ARCHITECTURES)\nCUDA_SRCS(answer.cu)\nEND()\n")
        self.write("answer.cu", "__global__ void answer(int* value) { *value = 42; }\n")
        node = self.node(self.graph(), "answer.50.cubin")
        args = [arg for command in node["cmds"] for arg in command["cmd_args"]]
        self.assertTrue(any("arch=compute_50,code=sm_50" in arg for arg in args), args)
