import re

from tst.make.lib import MakeTest, PROJECT


class CudaContextsTest(MakeTest):
    def args(self, node):
        # ya spells a resource root $(NAME-sbr:ID), ay $(B)/resources/NAME.
        roots = [arg.replace("$(SOURCE_ROOT)", "$(S)").replace("$(BUILD_ROOT)", "$(B)") for command in node["cmds"] for arg in command["cmd_args"]]
        return [re.sub(r"\$\((\w+)-[^)]*\)|\$\(B\)/resources/(\w+)", lambda m: f"$({m[1] or m[2]})", arg) for arg in roots]

    def test_cuda_outputs_in_pic_library(self):
        self.write("ya.make", f"DLL(answer)\nPEERDIR({PROJECT}/cuda)\nEND()\n")
        self.write("cuda/ya.make", "CUDA_DEVICE_LINK_LIBRARY()\nSRCS(answer.cu)\nNVCC_DEVICE_LINK(answer.cu)\nEND()\n")
        self.write("cuda/answer.cu", "__global__ void answer(int* value) { *value = 42; }\n")
        graph = self.graph()
        self.node(graph, "cuda/answer.cu.o")
        self.node(graph, "cuda/devlink.o")
        self.assertFalse(any(path.endswith(".cu.pic.o") for node in graph["graph"] for path in node["outputs"]))

    def test_cuda_source_peers_platform_into_module_flags(self):
        self.write("ya.make", "LIBRARY()\nSRCS(plain.c kernel.cu)\nEND()\n")
        self.write("plain.c", "int plain(void) { return 42; }\n")
        self.write("kernel.cu", "__global__ void kernel(int* value) { *value = 42; }\n")
        graph = self.graph()
        plain = self.args(self.node(graph, "plain.c.o"))
        self.assertIn("-I$(S)/contrib/libs/nvidia/cccl/cub", plain)
        self.assertIn("-I$(CUDA)/include", plain)
        self.assertTrue(any(arg.startswith("-DCUDA_VERSION_FOR_CACHE_INVALIDATION=") for arg in plain), plain)
        self.assertIn("$(CUDA)/bin/nvcc", self.args(self.node(graph, "kernel.cu.o")))

    def test_cuda_source_links_program_with_cuda_plugin(self):
        self.write("ya.make", f"PROGRAM(answer)\nSRCS(main.cpp)\nPEERDIR({PROJECT}/lib)\nEND()\n")
        self.write("main.cpp", "int main() { return 0; }\n")
        self.write("lib/ya.make", "LIBRARY()\nSRCS(kernel.cu)\nEND()\n")
        self.write("lib/kernel.cu", "__global__ void kernel(int* value) { *value = 42; }\n")
        graph = self.graph()
        link = self.node(graph, "answer")
        args = self.args(link)
        self.assertIn("$(B)/build/internal/platform/cuda/plugin/00-cuda.py.pyplugin", link["inputs"])
        self.assertIn("-L$(CUDA)/lib64", args)
        self.assertIn("-lcudart_static", args)
