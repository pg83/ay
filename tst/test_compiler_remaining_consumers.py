"""Native flag consumption outside C/C++ argument blocks."""
import unittest

import lib


def library(body):
    return "LIBRARY()\n" + lib.BARE_MODULE + body + "END()\n"


def fixture(body, **sources):
    return {
        "build/ymake_conf.py": lib.ymake_conf(extra='QUOTED=-DVALUE="two words"\n'),
        "grove/ya.make": library(body),
        **sources,
    }


def selected(node):
    return [a for a in node["cmds"][0]["cmd_args"]
            if a.startswith(("-DVALUE", "-DPROBE_"))]


def llvm_fixture(body, **sources):
    files = fixture("USE_LLVM_BC20()\n" + body, **sources)
    files["build/platform/clang/ya.make"] = lib.resource_lib("CLANG20", 741)
    files["build/platform/python/ymake_python3/ya.make"] = lib.resource_lib("YMAKE_PYTHON3", 742)
    files["library/cpp/resource/ya.make"] = library("")
    for tool in ("rescompiler", "rescompressor"):
        lib.tool_program(files, "tools/" + tool, "renamed-" + tool)
    return files


def make(files, *targets):
    return lib.make(files, *(targets or ("grove",)), "--target-platform", "default-linux-x86_64")


class RemainingCompilerConsumersTest(unittest.TestCase):
    def test_assembler_consumes_common_flags_and_ignores_private_language_buckets(self):
        for extension in ("S", "s"):
            with self.subTest(extension=extension):
                files = fixture(
                    "CFLAGS($QUOTED -DPROBE_LATE=$LATE -DPROBE_EMPTY=${ABSENT})\n"
                    "CXXFLAGS(-DBAD=${)\nCONLYFLAGS(-DBAD=${)\n"
                    "SET(LATE bound)\nSET(ABSENT later)\nSET_APPEND(SFLAGS -DASSEMBLER)\n"
                    f"SRC(leaf.{extension} -DPER_SOURCE)\n",
                    **{f"grove/leaf.{extension}": '#include "leaf.h"\n', "grove/leaf.h": ""},
                )
                graph = make(files)
                node = lib.node_by_output(graph, f"$(B)/grove/leaf.{extension}.o")
                self.assertEqual(selected(node), ["-DVALUE=two words", "-DPROBE_LATE=bound", "-DPROBE_EMPTY="])
                args = node["cmds"][0]["cmd_args"]
                self.assertLess(args.index("-DPROBE_LATE=bound"), args.index("-DASSEMBLER"))
                self.assertLess(args.index("-DASSEMBLER"), args.index("-DPER_SOURCE"))
                self.assertIn("--target=x86_64-linux-gnu", args)
                self.assertIn("-m64", args)
                self.assertIn(f"$(S)/grove/leaf.{extension}", node["inputs"])
                self.assertIn("$(S)/grove/leaf.h", node["inputs"])
                archive = lib.node_by_output(graph, "$(B)/grove/libgrove.a")
                self.assertIn(node["uid"], archive["deps"])
                self.assertIn(node["outputs"][0], archive["inputs"])

    def test_generated_assembly_keeps_tool_main_output_input_and_archive_edges(self):
        files = fixture(
            "CFLAGS($QUOTED -DPROBE_SOURCE=${ARCADIA_ROOT}/asset -DPROBE_OUTPUT=${BINDIR}/asset "
            "-DPROBE_RESOURCE=$MINT_RESOURCE_GLOBAL/include -DPROBE_LATE=$LATE)\n"
            "SET(LATE target)\nPEERDIR(resource)\nRUN_PROGRAM(tools/write IN seed.txt OUT generated.S)\n",
            **{"grove/seed.txt": "generated assembly\n", "resource/ya.make": lib.resource_lib("MINT", 743)},
        )
        lib.tool_program(files, "tools/write", "renamed-assembly-writer")
        graph = make(files)
        generated = lib.node_by_output(graph, "$(B)/grove/generated.S")
        tool = lib.node_by_output(graph, "$(B)/tools/write/renamed-assembly-writer")
        assembled = lib.node_by_output(graph, "$(B)/grove/generated.S.o")
        archive = lib.node_by_output(graph, "$(B)/grove/libgrove.a")
        self.assertEqual(selected(assembled), ["-DVALUE=two words", "-DPROBE_SOURCE=$(S)/asset",
                                             "-DPROBE_OUTPUT=$(B)/grove/asset",
                                             "-DPROBE_RESOURCE=$(B)/resources/MINT/include", "-DPROBE_LATE=target"])
        self.assertIn("$(S)/grove/seed.txt", generated["inputs"])
        self.assertIn(tool["outputs"][0], generated["inputs"])
        self.assertIn(tool["uid"], generated["deps"])
        self.assertIn(generated["outputs"][0], assembled["inputs"])
        self.assertIn(generated["uid"], assembled["deps"])
        self.assertIn(assembled["uid"], archive["deps"])
        lib.node_by_output(graph, "$(B)/resources/MINT")

    def test_shared_assembly_flags_keep_module_owners_and_root_orders(self):
        files = {"policy/asm.inc": "CFLAGS(-DPROBE_OWNER=$LATE)\n"}
        for directory, value in (("grove", "first"), ("marsh", "second")):
            files[f"{directory}/ya.make"] = library(
                "INCLUDE(${ARCADIA_ROOT}/policy/asm.inc)\n" +
                f"SET(LATE {value})\nSRCS(leaf.S)\n")
            files[f"{directory}/leaf.S"] = ""
        for targets in (("grove", "marsh"), ("marsh", "grove")):
            with self.subTest(targets=targets):
                graph = make(files, *targets)
                for directory, value in (("grove", "first"), ("marsh", "second")):
                    node = lib.node_by_output(graph, f"$(B)/{directory}/leaf.S.o")
                    self.assertEqual(selected(node), [f"-DPROBE_OWNER={value}"])

    def test_c_bitcode_uses_common_flags_without_cxx_or_conly_buckets(self):
        graph = make(llvm_fixture(
            "CFLAGS($QUOTED -DPROBE_LATE=$LATE)\nCXXFLAGS(-DBAD=${)\nCONLYFLAGS(-DBAD=${)\n"
            "LLVM_BC(plain.c NAME reed SUFFIX .renamed)\nSET(LATE bound)\n",
            **{"grove/plain.c": '#include "plain.h"\n', "grove/plain.h": ""},
        ))
        node = lib.node_by_output(graph, "$(B)/grove/plain.c.renamed.bc")
        self.assertEqual(selected(node), ["-DVALUE=two words", "-DPROBE_LATE=bound"])
        args = node["cmds"][0]["cmd_args"]
        self.assertIn("$(B)/resources/CLANG20/bin/clang", args)
        self.assertFalse(any(a.startswith("-std=c++") for a in args))
        self.assertEqual(args[-5:], ["-emit-llvm", "-c", "$(S)/grove/plain.c", "-o",
                                    "$(B)/grove/plain.c.renamed.bc"])
        self.assertIn("$(S)/grove/plain.c", node["inputs"])
        self.assertIn("$(S)/grove/plain.h", node["inputs"])
        merge = lib.node_by_output(graph, "$(B)/grove/reed_merged.renamed.bc")
        self.assertIn(node["uid"], merge["deps"])
        self.assertIn(node["outputs"][0], merge["inputs"])

    def test_cpp_bitcode_lowers_private_cxx_flags_and_keeps_pipeline_outputs(self):
        graph = make(llvm_fixture(
            "CFLAGS($QUOTED -DPROBE_LATE=$LATE)\nCXXFLAGS(-DPROBE_CXX=\"$LATE words\")\n"
            "CONLYFLAGS(-DBAD=${)\nLLVM_BC(branch.cpp NAME reed SUFFIX .renamed SYMBOLS entry)\n"
            "SET(LATE bound)\n", **{"grove/branch.cpp": "int branch;\n"},
        ))
        node = lib.node_by_output(graph, "$(B)/grove/branch.cpp.renamed.bc")
        self.assertEqual(selected(node), ["-DVALUE=two words", "-DPROBE_LATE=bound", "-DPROBE_CXX=bound words"])
        args = node["cmds"][0]["cmd_args"]
        self.assertIn("$(B)/resources/CLANG20/bin/clang++", args)
        self.assertIn("-std=c++20", args)
        self.assertEqual(args[-6], "-Wno-unknown-warning-option")
        merge = lib.node_by_output(graph, "$(B)/grove/reed_merged.renamed.bc")
        optimized = lib.node_by_output(graph, "$(B)/grove/reed_optimized.renamed.bc")
        self.assertIn(node["uid"], merge["deps"])
        self.assertIn(merge["uid"], optimized["deps"])
        self.assertIn(merge["outputs"][0], optimized["inputs"])
        self.assertIn("-internalize-public-api-list=entry", optimized["cmds"][0]["cmd_args"])
        packed = lib.node_by_output_prefix(graph, "$(B)/grove/objcopy_")
        self.assertIn(optimized["outputs"][0], packed["inputs"])
        self.assertIn(optimized["uid"], packed["deps"])

    def test_direct_cpp_bitcode_keeps_generated_source_and_real_tool_dependencies(self):
        files = llvm_fixture(
            "CFLAGS($QUOTED -DPROBE_LATE=$LATE)\n"
            "CXXFLAGS(-DPROBE_CXX=\"$LATE words\")\nCONLYFLAGS(-DBAD=${)\n"
            "RUN_PROGRAM(tools/write IN seed.txt OUT generated.cpp)\n"
            "LLVM_COMPILE_CXX(generated.cpp named.bc -DOPTION)\nRESOURCE(named.bc payload)\n"
            "SET(LATE bound)\n", **{"grove/seed.txt": "int generated;\n"},
        )
        lib.tool_program(files, "tools/write", "renamed-bitcode-writer")
        graph = make(files)
        generated = lib.node_by_output(graph, "$(B)/grove/generated.cpp")
        tool = lib.node_by_output(graph, "$(B)/tools/write/renamed-bitcode-writer")
        bitcode = lib.node_by_output(graph, "$(B)/grove/named.bc")
        self.assertEqual(selected(bitcode), ["-DVALUE=two words", "-DPROBE_LATE=bound", "-DPROBE_CXX=bound words"])
        self.assertIn("-DOPTION", bitcode["cmds"][0]["cmd_args"])
        self.assertIn(tool["outputs"][0], generated["inputs"])
        self.assertIn(tool["uid"], generated["deps"])
        self.assertIn(generated["outputs"][0], bitcode["inputs"])
        self.assertIn(generated["uid"], bitcode["deps"])

    def test_consumed_malformed_flags_remain_errors(self):
        for body, files in (
            ("CFLAGS(-DBAD=${)\nSRCS(leaf.S)\n", {"grove/leaf.S": ""}),
            ("CFLAGS(-DBAD=${)\nLLVM_BC(plain.c NAME reed)\n", {"grove/plain.c": ""}),
            ("CXXFLAGS(-DBAD=${)\nLLVM_BC(branch.cpp NAME reed)\n", {"grove/branch.cpp": ""}),
        ):
            with self.subTest(body=body):
                process = lib.make_process(llvm_fixture(body, **files), "grove")
                self.assertNotEqual(process.returncode, 0)
                self.assertIn("could not parse compiler command", process.stderr)


if __name__ == "__main__":
    unittest.main(verbosity=2)
