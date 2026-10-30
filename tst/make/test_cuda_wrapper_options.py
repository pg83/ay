from tst.make.lib import MakeTest


class CudaWrapperOptionsTest(MakeTest):
    def cuda(self, *flags):
        self.write("ya.make", "LIBRARY()\nSRCS(answer.cu)\nEND()\n")
        self.write("answer.cu", "__global__ void answer(int* value) { *value = 42; }\n")
        node = self.node(self.graph(*flags), "answer.cu.o")
        return [arg for command in node["cmds"] for arg in command["cmd_args"]]

    def test_clang_fourteen_skips_compressed_debug_information(self):
        self.assertIn("--y_skip_compressed_debug_info", self.cuda("-DCUDA_HOST_COMPILER_VERSION=14"))

    def test_ordinary_cuda_compilation_can_be_sanitized(self):
        self.assertIn("--y_sanitize", self.cuda("-DCUDA_SANITIZE=yes"))

    def test_architecture_without_a_suffix_is_rejected(self):
        self.write("ya.make", "LIBRARY()\nSET(CUDA_ARCHITECTURES sm_)\nCUDA_SRCS(answer.cu)\nEND()\n")
        self.write("answer.cu", "__global__ void answer(int* value) { *value = 42; }\n")
        self.assertNotEqual(self.make().returncode, 0)
