import unittest

import lib
import test_codegen_cuda
from test_yamake_flags import includes


class ModuleEdgeContractsTest(unittest.TestCase):
    def test_c_scoped_include_directories_propagate_only_when_global(self):
        files = {
            "app/ya.make": f"PROGRAM()\n{lib.BARE_MODULE}PEERDIR(dep)\nSRCS(main.cpp)\nEND()\n",
            "app/main.cpp": '#include "public.h"\n',
            "dep/ya.make": f"LIBRARY()\n{lib.BARE_MODULE}ADDINCL(FOR c private GLOBAL FOR c public)\nSRCS(dep.cpp)\nEND()\n",
            "dep/dep.cpp": '#include "private.h"\n',
            "private/private.h": "", "public/public.h": "",
        }
        graph = lib.make(files, "app")
        self.assertIn("-I$(S)/private", includes(graph, "$(B)/dep/dep.cpp.o"))
        self.assertIn("-I$(S)/public", includes(graph, "$(B)/app/main.cpp.o"))
        self.assertNotIn("-I$(S)/private", includes(graph, "$(B)/app/main.cpp.o"))

    def test_list_prefix_suffix_and_empty_append_expand_source_names(self):
        files = {
            "app/ya.make": f"LIBRARY()\n{lib.BARE_MODULE}SET_LIST_PRE_SUF(SOURCES pre_ .cpp first second)\n"
                "SET_APPEND(${MISSING})\nSRCS(${SOURCES})\nEND()\n",
            "app/pre_first.cpp": "", "app/pre_second.cpp": "",
        }
        graph = lib.make(files, "app")
        for name in ("first", "second"):
            obj = lib.node_by_output(graph, f"$(B)/app/pre_{name}.cpp.o")
            self.assertIn(f"$(S)/app/pre_{name}.cpp", obj["inputs"])

    def test_malformed_conditions_select_the_false_branch(self):
        for condition in ("yes no", "(yes no)", "<="):
            with self.subTest(condition=condition):
                files = {
                    "app/ya.make": f"LIBRARY()\n{lib.BARE_MODULE}IF({condition})\n"
                        "SRCS(wrong.cpp)\nELSE()\nSRCS(right.cpp)\nENDIF()\nEND()\n",
                    "app/right.cpp": "", "app/wrong.cpp": "",
                }
                graph = lib.make(files, "app")
                lib.node_by_output(graph, "$(B)/app/right.cpp.o")
                self.assertFalse(any("$(B)/app/wrong.cpp.o" in n["outputs"] for n in graph["graph"]))

    def test_enum_generation_requires_an_output_name(self):
        result = lib.make_process({"app/ya.make": f"LIBRARY()\n{lib.BARE_MODULE}"
            "ALICE_GENERATE_NOEXCEPT_ENUM_SERIALIZATION()\nEND()\n"}, "app")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("expects exactly 1 argument", result.stderr)
        self.assertNotIn("panic:", result.stderr)

    def test_cuda_default_architecture_and_extensionless_source(self):
        files = test_codegen_cuda.CudaTest().split_fixture()
        files["mod/ya.make"] = files["mod/ya.make"].replace("SET(CUDA_ARCHITECTURES sm_80:sm_90a)", "NO_COMPILER_WARNINGS()")
        files["mod/ya.make"] = files["mod/ya.make"].replace("sub/k.cu", "sub/kernel")
        files["mod/sub/kernel"] = files.pop("mod/sub/k.cu")
        graph = lib.make(files, "mod")
        device = lib.node_by_output(graph, "$(B)/mod/sub/kernel.50.cubin")
        args = device["cmds"][0]["cmd_args"]
        self.assertIn("arch=compute_50,code=sm_50", args)
        self.assertIn("-w", args)
        self.assertIn("$(B)/resources/CUDA/bin/nvcc", args)
        self.assertIn("$(S)/mod/sub/kernel", device["inputs"])

    def test_cuda_device_link_uses_the_internal_platform_resource(self):
        files = test_codegen_cuda.CudaTest().split_fixture()
        files["build/ymake_conf.py"] = test_codegen_cuda.cuda_conf("CUDA_ROOT=$INTERNAL_CUDA_RESOURCE_GLOBAL\nCUDA_TARGET_ROOT=$CUDA_ROOT\n")
        files["build/internal/platform/cuda/ya.make"] = lib.resource_lib("INTERNAL_CUDA", 17)
        files["mod/ya.make"] = f"CUDA_DEVICE_LINK_LIBRARY()\n{lib.BARE_MODULE}SRCS(sub/k.cu)\nNVCC_DEVICE_LINK(sub/k.cu)\nEND()\n"
        graph = lib.make(files, "mod")
        archive = lib.node_by_output(graph, "$(B)/mod/libmod.a")
        self.assertIn("$(B)/mod/devlink.o", archive["outputs"])
        self.assertEqual(archive["cmds"][0]["cmd_args"][0], "$(B)/resources/INTERNAL_CUDA/bin/nvcc")


if __name__ == "__main__":
    unittest.main(verbosity=2)
