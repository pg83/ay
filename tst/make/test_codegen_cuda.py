import unittest

from tst import lib
from tst.make.test_python2 import CONTRIB_LIBS_PYTHON, python2_base
from tst.make.test_python_srcs import python_base as python3_base


def cuda_conf(extra=""):
    return lib.ymake_conf(extra=(
        "CUDA_ROOT=$(B)/resources/CUDA\n"
        "CUDA_TARGET_ROOT=$CUDA_ROOT\n"
        "CUDA_SANITIZE=no\n"
        "CUDA_HOST_COMPILER=$(B)/resources/CUDA_HOST_TOOLCHAIN/bin/clang\n"
        "CUDA_NVCC_FLAGS=\n"
        "NVCC_STD_VER=20\n"
        "NVCC_FLAGS=-Xfatbin=-compress-all --expt-extended-lambda --expt-relaxed-constexpr "
        "--allow-unsupported-compiler --dont-use-profile --libdevice-directory=$CUDA_ROOT/nvvm/libdevice "
        "--keep --keep-dir=${BINDIR} --compiler-bindir=$CUDA_HOST_COMPILER "
        "-I$(B)/resources/OS_SDK_ROOT/usr/include/x86_64-linux-gnu $CUDA_NVCC_FLAGS\n"
    ) + extra)


class CudaTest(unittest.TestCase):
    def split_fixture(self, settings=""):
        files = {
            "build/ymake_conf.py": cuda_conf(),
            "mod/ya.make": (
                f"LIBRARY()\n{lib.BARE_MODULE}"
                "SET(CUDA_ARCHITECTURES sm_80:sm_90a)\n"
                f"{settings}\nCUDA_NVCC_FLAGS(-DFOO)\nCUDA_SRCS(sub/k.cu)\nEND()\n"
            ),
            "mod/sub/k.cu": '#include "k.h"\n',
            "mod/sub/k.h": "",
            "build/scripts/compile_cuda.py": "",
            "build/scripts/fatbinary_wrapper.py": "",
            "build/scripts/fs_tools.py": "",
            "build/internal/platform/cuda/cuda_runtime_include.h": '#include "rt.h"\n',
            "build/internal/platform/cuda/rt.h": "",
        }
        lib.tool_program(files, "tools/mtime0", "mtime0")
        lib.tool_program(files, "tools/custom_pid", "custom_pid")
        return files

    def test_split_cuda_compilation_and_generated_includes(self):
        graph = lib.make(self.split_fixture(), "mod", "--target-platform", "default-linux-x86_64")
        base = "$(B)/mod/sub/k"
        for arch in ("80", "90a"):
            device = lib.node_by_output(graph, f"{base}.{arch}.cubin")
            self.assertEqual(device["outputs"], [f"{base}.{arch}.{ext}" for ext in
                                               ("cubin", "ptx", "cudafe1.stub.c", "module_id")])
            args = device["cmds"][0]["cmd_args"]
            self.assertIn(f"arch=compute_{arch},code=sm_{arch}", args)
            self.assertEqual(args[-2:], ["-U__CUDA_ARCH_LIST__", "-D__CUDA_ARCH_LIST__=900"])
            self.assertIn("$(S)/mod/sub/k.h", device["inputs"])
            self.assertIn("$(S)/build/internal/platform/cuda/rt.h", device["inputs"])
            self.assertEqual(device["cmds"][1]["cmd_args"][-2:], ["k.ptx", f"{base}.{arch}.ptx"])
            self.assertEqual(device["cmds"][1]["cwd"], "$(B)/mod")
        fatbin = lib.node_by_output(graph, base + ".fatbin.c")
        self.assertEqual(fatbin["cmds"][0]["cmd_args"][-7:-1],
                         [f"{base}.{arch}.{ext}" for arch in ("80", "90a")
                          for ext in ("ptx", "cubin", "module_id")])
        host = lib.node_by_output(graph, base + ".cudafe1.cpp")
        self.assertEqual(host["outputs"], [base + ".ii", base + ".cudafe1.cpp"])
        self.assertIn("--preprocess", host["cmds"][0]["cmd_args"])
        args = host["cmds"][1]["cmd_args"]
        self.assertEqual(args[args.index("--stub_file_name") + 1], "k.90a.cudafe1.stub.c")
        cc = lib.node_by_output(graph, base + ".cudafe1.cpp.o")
        self.assertEqual(set(cc["inputs"]), {base + suffix for suffix in
                         (".ii", ".cudafe1.cpp", ".fatbin.c", ".90a.cudafe1.stub.c", ".90a.cubin")})
        self.assertIn("-D__NV_LEGACY_LAUNCH", cc["cmds"][0]["cmd_args"])
        self.assertIn(fatbin["uid"], cc["deps"])
        self.assertIn(host["uid"], cc["deps"])

    def test_cuda_uses_cxxflags_without_clang_warning_command_slots(self):
        for declaration in ("SRCS(sub/k.cu)", "CUDA_SRCS(sub/k.cu)"):
            with self.subTest(declaration=declaration):
                files = self.split_fixture()
                files["mod/ya.make"] = (
                    f"LIBRARY()\n{lib.BARE_MODULE}"
                    "SET(CUDA_ARCHITECTURES sm_80:sm_90a)\n"
                    "CLANG_WARNINGS(-Wcomment -Wunreachable-code)\n"
                    "CFLAGS(-DCOMMON)\nCXXFLAGS(-DCXX)\n"
                    f"SRCS(plain.c plain.cpp)\n{declaration}\nEND()\n"
                )
                files["mod/plain.c"] = ""
                files["mod/plain.cpp"] = ""
                graph = lib.make(files, "mod")
                for source in ("plain.c", "plain.cpp"):
                    args = lib.node_by_output(graph, f"$(B)/mod/{source}.o")["cmds"][0]["cmd_args"]
                    self.assertIn("-Wcomment", args)
                    self.assertIn("-Wunreachable-code", args)
                cuda = [node for node in graph["graph"] if node["kv"].get("p") == "CU"
                        and "--cflags" in node["cmds"][0]["cmd_args"]]
                self.assertTrue(cuda)
                for node in cuda:
                    args = node["cmds"][0]["cmd_args"]
                    self.assertIn("-DCOMMON", args)
                    self.assertIn("-DCXX", args)
                    self.assertNotIn("-Wcomment", args)
                    self.assertNotIn("-Wunreachable-code", args)

    def test_cuda_defaults_follow_the_source_configuration(self):
        files = self.split_fixture()
        files["build/ymake_conf.py"] = cuda_conf("CUDA_VERSION=12.9\n")
        files["mod/ya.make"] = (
            f"LIBRARY()\n{lib.BARE_MODULE}"
            "CUDA_NVCC_FLAGS(-DVERSION=$CUDA_VERSION)\n"
            "SRCS(sub/k.cu)\nEND()\n"
        )
        graph = lib.make(files, "mod")
        node = lib.node_by_output(graph, "$(B)/mod/_/sub/k.cu.o")
        self.assertIn("-DVERSION=12.9", node["cmds"][0]["cmd_args"])
        graph = lib.make(files, "mod", "-DCUDA_VERSION=13.0")
        node = lib.node_by_output(graph, "$(B)/mod/_/sub/k.cu.o")
        self.assertIn("-DVERSION=13.0", node["cmds"][0]["cmd_args"])

    def test_cuda_commands_expand_emitted_configuration_and_peer_resources(self):
        files = self.split_fixture()
        files["build/ymake_conf.py"] = cuda_conf(
            "CUDA_ROOT=$DEVICE_SDK_RESOURCE_GLOBAL\n"
            "CUDA_TARGET_ROOT=$CUDA_ROOT\n"
            "CUDA_ARCHITECTURES=sm_86:sm_90\n"
            "CUDA_VERSION=11.4\n"
            "NVCC_STD_VER=17\n"
            "CUDA_HOST_COMPILER_VERSION=14\n"
            "CUDA_NVCC_FLAGS=-DFROM_CONF\n"
            "NVCC_FLAGS=--compiler-bindir=$HOST_CC_RESOURCE_GLOBAL/bin/clang "
            "--keep-dir=${BINDIR} $CUDA_NVCC_FLAGS\n"
        )
        files["sdk/ya.make"] = (
            "RESOURCES_LIBRARY()\n"
            "DECLARE_EXTERNAL_RESOURCE(DEVICE_SDK sbr:1 HOST_CC sbr:2)\nEND()\n"
        )
        files["mod/ya.make"] = (
            f"LIBRARY()\n{lib.BARE_MODULE}PEERDIR(sdk)\n"
            "CUDA_NVCC_FLAGS(-DFROM_MACRO)\nCUDA_SRCS(sub/k.cu)\nEND()\n"
        )
        graph = lib.make(files, "mod", "--sandboxing")
        for arch in ("86", "90"):
            node = lib.node_by_output(graph, f"$(B)/mod/sub/k.{arch}.cubin")
            args = node["cmds"][0]["cmd_args"]
            self.assertIn("$(B)/resources/DEVICE_SDK/bin/nvcc", args)
            self.assertIn("--y_skip_compressed_debug_info", args)
            self.assertIn("-std=c++17", args)
            start = args.index("--compiler-bindir=$(B)/resources/HOST_CC/bin/clang")
            self.assertEqual(args[start:start + 4], [
                "--compiler-bindir=$(B)/resources/HOST_CC/bin/clang",
                "--keep-dir=$(B)/mod", "-DFROM_CONF", "-DFROM_MACRO",
            ])
            self.assertNotIn("-Xfatbin=-compress-all", args)
            self.assertFalse(any(arg.startswith("-Xcicc=--orig_src_path_name=") for arg in args))
            self.assertEqual(node["env"]["PATH"],
                             "$(B)/resources/DEVICE_SDK/nvvm/bin:$(B)/resources/DEVICE_SDK/bin")

    def test_no_embed_ptx_omits_only_fatbinary_images(self):
        graph = lib.make(self.split_fixture("SET(CUDA_NO_EMBED_PTX yes)"), "mod")
        fatbin = lib.node_by_output(graph, "$(B)/mod/sub/k.fatbin.c")
        self.assertFalse(any(x.endswith(".ptx") for x in fatbin["cmds"][0]["cmd_args"]))
        lib.node_by_output(graph, "$(B)/mod/sub/k.80.ptx")

    def test_macro_header_alias_uses_system_include_resolution(self):
        files = self.split_fixture()
        files["mod/sub/k.h"] = "#include DEVICE_HEADER(tuple)\n"
        files["build/sysincl/macro.yml"] = (
            '- source_filter: "^mod/"\n  includes:\n'
            '    - DEVICE_HEADER(tuple): $U/device/tuple\n'
        )
        files["build/sysincl/misc.yml"] = (
            '- includes:\n    - device/tuple: sdk/tuple\n'
        )
        files["sdk/tuple"] = '#include "types.h"\n'
        files["sdk/types.h"] = ""
        graph = lib.make(files, "mod")
        device = lib.node_by_output(graph, "$(B)/mod/sub/k.80.cubin")
        self.assertIn("$(S)/sdk/tuple", device["inputs"])
        self.assertIn("$(S)/sdk/types.h", device["inputs"])

    def test_device_link_is_part_of_archive_command(self):
        files = self.split_fixture()
        files["mod/ya.make"] = (
            f"CUDA_DEVICE_LINK_LIBRARY()\n{lib.BARE_MODULE}"
            "CUDA_NVCC_FLAGS(--device-c)\nSRCS(sub/k.cu)\nNVCC_DEVICE_LINK(sub/k.cu)\nEND()\n"
        )
        graph = lib.make(files, "mod", "--target-platform", "default-linux-x86_64")
        ar = lib.node_by_output(graph, "$(B)/mod/libmod.a")
        self.assertEqual(ar["outputs"], ["$(B)/mod/libmod.a", "$(B)/mod/devlink.o"])
        self.assertEqual(len(ar["cmds"]), 2)
        args = ar["cmds"][0]["cmd_args"]
        self.assertEqual(args[0], "$(B)/resources/CUDA/bin/nvcc")
        self.assertEqual(args[args.index("-dlink") + 1], "$(B)/mod/_/sub/k.cu.o")
        self.assertEqual(ar["cmds"][1]["cmd_args"][-1], "$(B)/mod/devlink.o")
        self.assertNotIn("$(B)/mod/devlink.o", ar["inputs"])

    def test_cuda_warning_suppression_is_local_to_each_python_context(self):
        sources = self.split_fixture()
        sources.update(python2_base())
        sources.update(python3_base())
        sources["contrib/libs/python/ya.make"] = CONTRIB_LIBS_PYTHON
        sources["build/ymake_conf.py"] = cuda_conf("CUDA_NVCC_FLAGS=-DFROM_CONF\n")
        sources["lib/ya.make"] = (
            f"PY23_LIBRARY()\n{lib.BARE_MODULE}"
            "NO_COMPILER_WARNINGS()\nNO_COMPILER_WARNINGS()\n"
            "CUDA_NVCC_FLAGS(-DCONTEXT)\nSRCS(k.cu)\nEND()\n"
        )
        sources["lib/k.cu"] = ""
        for directory, module in (("two", "PY2_PROGRAM"), ("three", "PY3_PROGRAM")):
            sources[directory + "/ya.make"] = (
                f"{module}()\n{lib.BARE_MODULE}PY_SRCS(MAIN main.py)\nPEERDIR(lib)\nEND()\n"
            )
            sources[directory + "/main.py"] = ""
        graph = lib.make(sources, "two", "three", "--sandboxing")
        compiled = [n for n in graph["graph"] if n["kv"]["p"] == "CU"]
        self.assertEqual(len(compiled), 2)
        contexts = set()
        for node in compiled:
            args = node["cmds"][0]["cmd_args"]
            contexts.update(flag for flag in args if flag in ("-DUSE_PYTHON2", "-DUSE_PYTHON3"))
            self.assertEqual(args[args.index("-DFROM_CONF"):args.index("-c")],
                             ["-DFROM_CONF", "-w", "-DCONTEXT"])
            self.assertTrue(any(node["uid"] in n["deps"] for n in graph["graph"] if n["kv"]["p"] == "AR"))
        self.assertEqual(contexts, {"-DUSE_PYTHON2", "-DUSE_PYTHON3"})

    def test_cuda_warning_suppression_keeps_macro_execution_order(self):
        for declarations, expected in (
            ("NO_COMPILER_WARNINGS()\nINCLUDE(${ARCADIA_ROOT}/flags.inc)\n",
             ["-w", "-DFIRST", "-DSECOND"]),
            ("CUDA_NVCC_FLAGS(-DFIRST)\nNO_COMPILER_WARNINGS()\nCUDA_NVCC_FLAGS(-DSECOND)\n",
             ["-DFIRST", "-w", "-DSECOND"]),
            ("INCLUDE(${ARCADIA_ROOT}/flags.inc)\nNO_COMPILER_WARNINGS()\n",
             ["-DFIRST", "-DSECOND", "-w"]),
            ("NO_COMPILER_WARNINGS()\nNO_COMPILER_WARNINGS()\nINCLUDE(${ARCADIA_ROOT}/flags.inc)\n",
             ["-w", "-DFIRST", "-DSECOND"]),
        ):
            with self.subTest(declarations=declarations):
                sources = self.split_fixture()
                sources["build/ymake_conf.py"] = cuda_conf("CUDA_NVCC_FLAGS=-DFROM_CONF\n")
                sources["flags.inc"] = "CUDA_NVCC_FLAGS(-DFIRST -DSECOND)\n"
                sources["mod/ya.make"] = (
                    f"LIBRARY()\n{lib.BARE_MODULE}" + declarations + "SRCS(k.cu)\nEND()\n"
                )
                sources["mod/k.cu"] = ""
                graph = lib.make(sources, "mod", "--sandboxing")
                node = lib.node_by_output(graph, "$(B)/mod/k.cu.o")
                args = node["cmds"][0]["cmd_args"]
                self.assertEqual(args[args.index("-DFROM_CONF"):args.index("-c")],
                                 ["-DFROM_CONF", *expected])

    def test_cuda_source_compiles_through_nvcc_wrapper(self):
        files = {
            "build/ymake_conf.py": cuda_conf(),
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
