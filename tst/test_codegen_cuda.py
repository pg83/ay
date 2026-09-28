import unittest

import lib


class CudaTest(unittest.TestCase):
    def test_cuda_source_compiles_through_nvcc_wrapper(self):
        files = {
            "mod/ya.make": (
                "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
                "CUDA_NVCC_FLAGS(-DFOO --use_fast_math)\n"
                "SRCS(k.cu)\nEND()\n"
            ),
            "mod/k.cu": '#include "k.h"\n',
            "mod/k.h": "",
            "build/scripts/compile_cuda.py": "",
            "build/internal/platform/cuda/cuda_runtime_include.h": '#include "rt2.h"\n',
            "build/internal/platform/cuda/rt2.h": "",
        }
        lib.tool_program(files, "tools/mtime0", "mtime0")
        lib.tool_program(files, "tools/custom_pid", "custom_pid")
        graph = lib.make(files, "mod", "--target-platform", "default-linux-x86_64")

        node = lib.node_by_output(graph, "$(B)/mod/k.cu.o")
        self.assertEqual(node["kv"], {"p": "CU", "pc": "light-green"})
        args = node["cmds"][0]["cmd_args"]
        self.assertEqual(args[:9], [
            "$(B)/resources/YMAKE_PYTHON3/bin/python3",
            "$(S)/build/scripts/compile_cuda.py",
            "--mtime", "$(B)/tools/mtime0/mtime0",
            "--custom-pid", "$(B)/tools/custom_pid/custom_pid",
            "$(B)/resources/CUDA/bin/nvcc",
            "-std=c++20",
            "-Xfatbin=-compress-all",
        ])
        start = args.index("--keep-dir=$(B)/mod")
        self.assertEqual(args[start:start + 11], [
            "--keep-dir=$(B)/mod",
            "--compiler-bindir=$(B)/resources/CUDA_HOST_TOOLCHAIN/bin/clang",
            "-I$(B)/resources/OS_SDK_ROOT/usr/include/x86_64-linux-gnu",
            "-DFOO", "--use_fast_math",
            "-c", "$(S)/mod/k.cu", "-o", "$(B)/mod/k.cu.o",
            "-I$(B)", "-I$(S)",
        ])
        self.assertEqual(args[start + 11:start + 13], ["--cflags", "--target=x86_64-linux-gnu"])
        self.assertEqual(args[-1], "-std=c++20")
        self.assertEqual(node["env"], {
            "ARCADIA_ROOT_DISTBUILD": "$(S)",
            "PATH": "$(B)/resources/CUDA/nvvm/bin:$(B)/resources/CUDA/bin",
        })
        self.assertEqual(node["inputs"], [
            "$(S)/build/scripts/compile_cuda.py",
            "$(B)/tools/mtime0/mtime0",
            "$(B)/tools/custom_pid/custom_pid",
            "$(S)/mod/k.cu",
            "$(S)/mod/k.h",
            "$(S)/build/internal/platform/cuda/cuda_runtime_include.h",
            "$(S)/build/internal/platform/cuda/rt2.h",
        ])
        mtime = lib.node_by_output(graph, "$(B)/tools/mtime0/mtime0")
        pid = lib.node_by_output(graph, "$(B)/tools/custom_pid/custom_pid")
        self.assertEqual(node["deps"], [mtime["uid"], pid["uid"]])
        archive = lib.node_by_output(graph, "$(B)/mod/libmod.a")
        self.assertIn("$(B)/mod/k.cu.o", archive["inputs"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
