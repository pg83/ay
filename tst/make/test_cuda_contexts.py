import re

from tst.make.lib import MakeTest, PROJECT


class CudaContextsTest(MakeTest):
    def args(self, node):
        # ya spells a resource root $(NAME-sbr:ID), ay $(B)/resources/NAME.
        roots = [arg.replace("$(SOURCE_ROOT)", "$(S)").replace("$(BUILD_ROOT)", "$(B)") for command in node["cmds"] for arg in command["cmd_args"]]
        return [re.sub(r"\$\((\w+)-[^)]*\)|\$\(B\)/resources/(\w+)", lambda m: f"$({m[1] or m[2]})", arg) for arg in roots]

    def test_cuda_warning_condition(self):
        self.write("ya.make", 'LIBRARY()\nNO_COMPILER_WARNINGS()\nIF(NO_COMPILER_WARNINGS == "yes")\nSRCS(answer.cu)\nELSE()\nSRCS(wrong.cpp)\nENDIF()\nEND()\n')
        self.write("answer.cu", "__global__ void answer(int* value) { *value = 42; }\n")
        self.write("wrong.cpp", "int wrong() { return 0; }\n")
        node = self.node(self.graph(), "answer.cu.o")
        args = [arg for cmd in node["cmds"] for arg in cmd["cmd_args"]]
        self.assertEqual(args.count("-w"), 1)

    def test_cuda_source_resources_in_test_metadata(self):
        self.write("ya.make", f"GTEST()\nPEERDIR({PROJECT}/cuda)\nDATA(sbr://123456789)\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "#include <library/cpp/testing/gtest/gtest.h>\nTEST(Answer, Value) {}\n")
        self.write("cuda/ya.make", "LIBRARY()\nSRCS(answer.cu)\nEND()\n")
        self.write("cuda/answer.cu", "__global__ void answer(int* value) { *value = 42; }\n")
        graph = self.graph("-t")
        for suite in ("gtest", "validate_data_sbr"):
            meta = next(node for node in graph["graph"] if any(path.endswith(f"/test-results/{suite}/meta.json") for path in node["outputs"]))
            args = [arg for cmd in meta["cmds"] for arg in cmd["cmd_args"]]
            for resource in ("CUDA_HOST_TOOLCHAIN", "CUDA"):
                self.assertTrue(any(arg.startswith(f"{resource}_RESOURCE_GLOBAL::$(") for arg in args), (suite, resource))

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

    def test_repeated_sysincl_macro_header_keeps_every_target(self):
        self.write("ya.make", "LIBRARY()\nSRCS(resource.cpp kernel.cu)\nEND()\n")
        self.write("resource.cpp", "#include <thrust/mr/device_memory_resource.h>\n")
        self.write("kernel.cu", "__global__ void kernel(int* value) { *value = 42; }\n")
        graph = self.graph()
        inputs = self.node(graph, "resource.cpp.o")["inputs"]
        system = "$(S)/contrib/libs/nvidia/cccl/thrust/thrust/system"
        self.assertIn(f"{system}/cuda/memory_resource.h", inputs)
        self.assertIn(f"{system}/cuda/memory.h", inputs)
        self.assertIn(f"{system}/tbb/memory.h", inputs)

    def test_cuda_source_runtime_header_is_cpp_style_input(self):
        self.write("ya.make", "LIBRARY()\nSTYLE_CPP()\nSRCS(plain.cpp kernel.cu)\nEND()\n")
        self.write("plain.cpp", "int plain() { return 42; }\n")
        self.write("kernel.cu", "__global__ void kernel(int* value) { *value = 42; }\n")
        node = self.node(self.graph("-t"), "test-results/clang_format/meta.json")
        self.assertIn("$(S)/build/internal/platform/cuda/cuda_runtime_include.h", node["inputs"])
        self.assertIn("$(S)/build/internal/platform/cuda/cuda_runtime_include.h", self.args(node))

    def test_cuda_nvcc_flags_survive_no_compiler_warnings(self):
        self.write("ya.make", "LIBRARY()\nCUDA_NVCC_FLAGS(-gencode=arch=compute_80,code=sm_80)\nNO_COMPILER_WARNINGS()\nSRCS(kernel.cu)\nEND()\n")
        self.write("kernel.cu", "__global__ void kernel(int* value) { *value = 42; }\n")
        args = self.args(self.node(self.graph(), "kernel.cu.o"))
        self.assertLess(args.index("-gencode=arch=compute_80,code=sm_80"), args.index("-w"))
