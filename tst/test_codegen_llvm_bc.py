import unittest

import lib


PYTHON = "$(B)/resources/YMAKE_PYTHON3/bin/python3"
CLANG = "$(B)/resources/CLANG20"


def llvm_bc_files(body, sources):
    files = {
        "mod/ya.make": (
            "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nUSE_LLVM_BC20()\n"
            + body + "END()\n"
        ),
        "build/platform/clang/ya.make": (
            "RESOURCES_LIBRARY()\nDECLARE_EXTERNAL_RESOURCE(CLANG20 sbr:1)\nEND()\n"
        ),
        "build/platform/python/ymake_python3/ya.make": (
            "RESOURCES_LIBRARY()\nDECLARE_EXTERNAL_RESOURCE(YMAKE_PYTHON3 sbr:2)\nEND()\n"
        ),
        **sources,
    }
    lib.tool_program(files, "tools/rescompiler", "rescompiler")
    lib.tool_program(files, "tools/rescompressor", "rescompressor")
    return files


def make(files):
    return lib.make(files, "mod", "--target-platform", "default-linux-x86_64")


def nodes_of_kind(graph, kind):
    return [node for node in graph["graph"] if node["kv"].get("p") == kind]


class LlvmBcTest(unittest.TestCase):
    def test_sources_compile_link_optimize_and_embed_as_resource(self):
        graph = make(llvm_bc_files(
            "PEERDIR(contrib/libs/googleapis-common-protos)\n"
            "LLVM_BC(a.cpp b.c NAME foo SYMBOLS x y SUFFIX .s1)\n",
            {
                "mod/a.cpp": '#include "a.h"\n',
                "mod/a.h": "",
                "mod/b.c": '#include "build/scripts/fs_tools.py"\n',
                "build/scripts/fs_tools.py": "",
                "contrib/libs/googleapis-common-protos/ya.make": (
                    "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
                    "ADDINCL(\n"
                    "  GLOBAL ${ARCADIA_BUILD_ROOT}/contrib/libs/googleapis-common-protos\n"
                    "  GLOBAL contrib/libs/googleapis-common-protos/inc\n"
                    ")\nEND()\n"
                ),
                "contrib/libs/googleapis-common-protos/inc/x.h": "",
            },
        ))

        compile_a = lib.node_by_output(graph, "$(B)/mod/a.cpp.s1.bc")
        self.assertEqual(compile_a["kv"], {"p": "BC", "pc": "light-green"})
        args = compile_a["cmds"][0]["cmd_args"]
        self.assertEqual(args[:6], [
            PYTHON, "$(S)/build/scripts/clang_wrapper.py", "no",
            f"{CLANG}/bin/clang++", "-I$(B)", "-I$(S)",
        ])
        self.assertEqual(args[6:8], [
            "-I$(B)/contrib/libs/googleapis-common-protos",
            "-I$(S)/contrib/libs/googleapis-common-protos/inc",
        ])
        self.assertEqual(args[-6:], [
            "-Wno-unknown-warning-option", "-emit-llvm", "-c",
            "$(S)/mod/a.cpp", "-o", "$(B)/mod/a.cpp.s1.bc",
        ])
        self.assertEqual(compile_a["inputs"], [
            "$(S)/build/scripts/clang_wrapper.py", "$(S)/mod/a.cpp", "$(S)/mod/a.h",
        ])
        compile_b = lib.node_by_output(graph, "$(B)/mod/b.c.s1.bc")

        merge = lib.node_by_output(graph, "$(B)/mod/foo_merged.s1.bc")
        self.assertEqual(merge["kv"], {"p": "LD", "pc": "light-red"})
        self.assertEqual(merge["cmds"][0]["cmd_args"], [
            f"{CLANG}/bin/llvm-link",
            "$(B)/mod/a.cpp.s1.bc", "$(B)/mod/b.c.s1.bc",
            "-o", "$(B)/mod/foo_merged.s1.bc",
        ])
        self.assertEqual(merge["inputs"], [
            "$(B)/mod/a.cpp.s1.bc", "$(B)/mod/b.c.s1.bc",
            "$(S)/build/scripts/fs_tools.py",
        ])
        self.assertIn(compile_a["uid"], merge["deps"])
        self.assertIn(compile_b["uid"], merge["deps"])

        optimize = lib.node_by_output(graph, "$(B)/mod/foo_optimized.s1.bc")
        self.assertEqual(optimize["kv"], {"p": "OP", "pc": "yellow"})
        self.assertEqual(optimize["cmds"][0]["cmd_args"], [
            PYTHON, "$(S)/build/scripts/llvm_opt_wrapper.py", f"{CLANG}/bin/opt",
            "$(B)/mod/foo_merged.s1.bc", "-o", "$(B)/mod/foo_optimized.s1.bc",
            "-internalize-public-api-list=x#y",
            '-passes="default<O2>,globalopt,globaldce,internalize"',
        ])
        self.assertEqual(optimize["inputs"][:2], [
            "$(B)/mod/foo_merged.s1.bc", "$(S)/build/scripts/llvm_opt_wrapper.py",
        ])
        for source in ("$(S)/mod/a.cpp", "$(S)/mod/a.h", "$(S)/mod/b.c"):
            self.assertIn(source, optimize["inputs"])
        self.assertIn(merge["uid"], optimize["deps"])

        objcopy = lib.node_by_output_prefix(graph, "$(B)/mod/objcopy_")
        args = objcopy["cmds"][0]["cmd_args"]
        self.assertEqual(args[args.index("--inputs") + 1], "$(B)/mod/foo_optimized.s1.bc")
        self.assertEqual(args[args.index("--keys") + 1], "L2xsdm1fYmMvZm9v")

    def test_copied_source_keeps_root_relative_bitcode_path(self):
        graph = make(llvm_bc_files(
            "COPY_FILE(tpl.txt gen.cpp)\nLLVM_BC(gen.cpp NAME bar)\n",
            {"mod/tpl.txt": ""},
        ))
        copy = lib.node_by_output(graph, "$(B)/mod/gen.cpp")
        compile_node = lib.node_by_output(graph, "$(B)/gen.cpp.bc")
        self.assertEqual(compile_node["cmds"][0]["cmd_args"][-3:], [
            "$(B)/mod/gen.cpp", "-o", "$(B)/gen.cpp.bc",
        ])
        self.assertEqual(compile_node["inputs"], [
            "$(S)/build/scripts/clang_wrapper.py", "$(B)/mod/gen.cpp",
        ])
        self.assertIn(copy["uid"], compile_node["deps"])
        merge = lib.node_by_output(graph, "$(B)/mod/bar_merged.bc")
        self.assertEqual(merge["inputs"], ["$(B)/gen.cpp.bc"])
        optimize = lib.node_by_output(graph, "$(B)/mod/bar_optimized.bc")
        self.assertEqual(
            optimize["cmds"][0]["cmd_args"][-1],
            '-passes="default<O2>,globalopt,globaldce"',
        )

    def test_generate_machine_code_registers_no_bitcode_nodes(self):
        graph = make(llvm_bc_files(
            "LLVM_BC(a.cpp NAME foo GENERATE_MACHINE_CODE)\n", {"mod/a.cpp": ""},
        ))
        for kind in ("BC", "OP"):
            self.assertEqual(nodes_of_kind(graph, kind), [])
        self.assertFalse(any(
            output.endswith(".bc")
            for node in graph["graph"] for output in node["outputs"]
        ))

    def test_platform_cxxflags_and_unresolved_source_name(self):
        files = llvm_bc_files("LLVM_BC(a.cpp missing.cpp NAME foo)\n", {
            "mod/a.cpp": "",
            "build/internal/ya.conf": '[flags]\nCXXFLAGS = "-DFROM_INTERNAL_CONF"\n',
        })
        graph = make(files)
        compile_a = lib.node_by_output(graph, "$(B)/mod/a.cpp.bc")
        args = compile_a["cmds"][0]["cmd_args"]
        self.assertEqual(args.count("-DFROM_INTERNAL_CONF"), 1)
        self.assertLess(args.index("-DFROM_INTERNAL_CONF"), args.index("-emit-llvm"))
        missing = lib.node_by_output(graph, "$(B)/missing.cpp.bc")
        self.assertEqual(missing["inputs"], [
            "$(S)/build/scripts/clang_wrapper.py", "$(S)/mod/missing.cpp",
        ])
        merge = lib.node_by_output(graph, "$(B)/mod/foo_merged.bc")
        self.assertEqual(merge["inputs"], ["$(B)/mod/a.cpp.bc", "$(B)/missing.cpp.bc"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
