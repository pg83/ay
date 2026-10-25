"""Late SOURCE bindings must reach commands emitted for an earlier root.

The first module forbids creating include bindings. The second root consumes
the same physical file with resolution enabled. Its command establishes the
bindings; the first command must observe them when its inputs are exported.

An upstream-confirmed fixture stays here when ay fails before the input
assertion, including configuration errors and missing command producers.
"""

from dataclasses import dataclass
import unittest

from tst import lib
from tst.make.test_bpf import fixture as bpf_files
from tst.make.test_codegen_enum import enum_files
from tst.make.test_codegen_llvm_bc import llvm_bc_files
from tst.make.test_codegen_bison_flex import bison_files
from tst.make.test_fortran import world as fortran_files


BARE = lib.BARE_MODULE


@dataclass(frozen=True)
class Case:
    name: str
    emit_site: str
    body: str
    source: str
    output: str
    module: str = "LIBRARY()"
    factory: str = "plain"
    source_path: str = ""
    source_content: str = ""
    leaf: str = "leaf.h"
    later_module: str = "LIBRARY()"
    owner_output: str = "owner.cpp.o"


CASES = (
    Case("cc", "emit_cc.go:370", "SRCS(input.cpp)", "input.cpp", "input.cpp.o"),
    Case("as", "emit_as.go:32", "SRCS(input.S)", "input.S", "input.S.o"),
    Case("run_program", "emit_pr.go:714", "RUN_PROGRAM(tools/reader IN input.h OUT result.dat)",
         "input.h", "result.dat", "UNION()"),
    Case("run_python", "emit_py_run.go:522", "RUN_PYTHON3(script.py IN input.h OUT result.dat)",
         "input.h", "result.dat", "UNION()"),
    Case("resource", "emit_resource.go:196", "RESOURCE(input.h payload)",
         "input.h", "objcopy_", factory="resource"),
    Case("configure", "emit_cf.go:78", "SRCS(input.cpp.in)",
         "input.cpp.in", "input.cpp"),
    Case("check_config", "emit_check_config_h.go:30", "CHECK_CONFIG_H(input.h)",
         "input.h", "input.config.cpp"),
    Case("enum", "emit_en.go:233", "GENERATE_ENUM_SERIALIZATION_WITH_HEADER(input.h)",
         "input.h", "input.h_serialized.cpp", factory="enum"),
    Case("alice_enum", "emit_en.go:156", "ALICE_GENERATE_NOEXCEPT_ENUM_SERIALIZATION(input.h)\nRESOURCE(${BINDIR}/input.h_serialized.h payload)",
         "input.h", "input.h_serialized.h", factory="alice_enum"),
    Case("bpf", "emit_llvm_bc.go:69", "BPF(input.c result.bpf)\nRESOURCE(result.bpf payload)",
         "input.c", "result.bpf", factory="bpf"),
    Case("bpf_static", "emit_llvm_bc.go:69", "BPF_STATIC(input.c result.bpf)",
         "input.c", "result.bpf", factory="bpf"),
    Case("llvm_compile", "emit_llvm_bc.go:69", "LLVM_COMPILE_CXX(input.cpp result.bc)\nRESOURCE(result.bc payload)",
         "input.cpp", "result.bc", factory="llvm"),
    Case("llvm_bc", "emit_llvm_bc.go:200", "LLVM_BC(input.cpp NAME result)",
         "input.cpp", "input.cpp.bc", factory="llvm"),
    Case("archive", "emit_archives.go:112", "ARCHIVE(NAME result.inc input.h)\nRESOURCE(result.inc payload)",
         "input.h", "result.inc", factory="resource"),
    Case("archive_asm", "emit_archive_asm.go:113", "ARCHIVE_ASM(input.h NAME result)",
         "input.h", "result.rodata", factory="resource"),
    Case("base_codegen", "emit_base_codegen.go:62", "BASE_CODEGEN(tools/codegen input.h)\nRESOURCE(${BINDIR}/input.h.h payload)",
         "input.h.in", "input.h.cpp", factory="resource"),
    Case("split_codegen", "emit_split_codegen.go:90", "SPLIT_CODEGEN(tools/codegen input.h NWorld)",
         "input.h.in", "input.h.0.cpp"),
    Case("decimal_md5", "emit_decimal_md5.go:61", "COPY_FILE_WITH_CONTEXT(input.h.in prepared.h.in)\nDECIMAL_MD5_LOWER_32_BITS(hash.cpp FUNCNAME hash ${BINDIR}/prepared.h.in)",
         "input.h.in", "hash.cpp"),
    Case("rodata", "emit_rodata.go:65", "COPY_FILE_WITH_CONTEXT(input.h.in prepared.rodata)\nSRCS(${BINDIR}/prepared.rodata)",
         "input.h.in", "prepared.rodata.o"),
    Case("copy", "emit_cp.go:8", "COPY_FILE_WITH_CONTEXT(input.h.in prepared.h.in)\nCOPY_FILE(${BINDIR}/prepared.h.in copied.h.in)",
         "input.h.in", "copied.h.in", module="UNION()"),
    Case("build_mn", "emit_build_mn.go:47", "BUILD_MN(input.h model)",
         "input.h", "mn.model.cpp", factory="mn"),
    Case("bison", "emit_bison_y.go:142", "SRCS(input.y)",
         "input.y", "input.y.cpp", factory="bison",
         source_path="contrib/tools/bison/data/skeletons/lalr1.cc"),
    Case("pch", "emit_pch.go:44", "PEERDIR(build/platform/clang)\nGEN_PCH_FILE(mod/input.h ${BINDIR}/input.h.pch)\nRESOURCE(${BINDIR}/input.h.pch payload)",
         "input.h", "input.h.pch", factory="pch"),
    Case("yasm", "emit_yasm.go:178", "SRCS(input.asm)",
         "input.asm", "input.o", source_content='%include "mod/leaf.inc"\n', leaf="leaf.inc"),
    Case("fortran", "emit_fortran.go:85", "SRCS(input.f)",
         "input.f", "input.f.o", factory="fortran", source_content="      include 'leaf.inc'\n", leaf="leaf.inc"),
    Case("ar", "emit_ar.go:28", "COPY_FILE_WITH_CONTEXT(input.h.in prepared.o)",
         "input.h.in", "libmod.a"),
    Case("ar_global", "emit_ar.go:176", "COPY_FILE_WITH_CONTEXT(input.h.in prepared.o)\nSRCS(GLOBAL ${BINDIR}/prepared.o)",
         "input.h.in", "libmod.global.a"),
    Case("ld", "emit_ld.go:309", "COPY_FILE_WITH_CONTEXT(input.h.in prepared.o)",
         "input.h.in", "app", module="PROGRAM(app)"),
    Case("dynamic_library", "emit_dynamic_library.go:329", "COPY_FILE_WITH_CONTEXT(input.h.in prepared.o)",
         "input.h.in", "libplugin.so", module="DLL(plugin)", later_module="DLL(owner)", owner_output="owner.cpp.pic.o"),
    Case("flex", "emit_flex.go:68", "COPY_FILE_WITH_CONTEXT(input.h.in prepared.l)\nSRCS(${BINDIR}/prepared.l)",
         "input.h.in", "prepared.l.cpp", factory="tool_sources"),
    Case("gperf", "emit_gperf.go:50", "COPY_FILE_WITH_CONTEXT(input.h.in prepared.gperf)\nSRCS(${BINDIR}/prepared.gperf)",
         "input.h.in", "prepared.gperf.cpp", factory="tool_sources"),
    Case("ragel5", "emit_r5.go:53", "COPY_FILE_WITH_CONTEXT(input.h.in prepared.rl)\nSRCS(${BINDIR}/prepared.rl)",
         "input.h.in", "prepared.rl5.cpp", factory="tool_sources"),
    Case("ragel6", "emit_r6.go:76", "COPY_FILE_WITH_CONTEXT(input.h.in prepared.rl6)\nSRCS(${BINDIR}/prepared.rl6)",
         "input.h.in", "prepared.rl6.cpp", factory="tool_sources"),
    Case("asp", "emit_ht.go:31", "COPY_FILE_WITH_CONTEXT(input.h.in prepared.asp)\nSRCS(${BINDIR}/prepared.asp)",
         "input.h.in", "prepared.asp.cpp", factory="tool_sources"),
    Case("sc", "emit_sc.go:58", "COPY_FILE_WITH_CONTEXT(input.h.in prepared.sc)\nSRCS(prepared.sc)\nRESOURCE(${BINDIR}/prepared.sc.h payload)",
         "input.h.in", "prepared.sc.h", factory="tool_sources"),
    Case("sfdl", "emit_sfdl.go:62", "COPY_FILE_WITH_CONTEXT(input.h.in prepared.inc.sfdl)\nSRCS(prepared.inc.sfdl)\nRESOURCE(${BINDIR}/prepared.inc payload)",
         "input.h.in", "prepared.inc", factory="tool_sources"),
    Case("fml", "emit_fml.go:34", "COPY_FILE_WITH_CONTEXT(input.h.in prepared.fml)\nSRCS(prepared.fml)\nRESOURCE(${BINDIR}/prepared.fml.inc payload)",
         "input.h.in", "prepared.fml.inc", factory="tool_sources"),
)


def fixture(case):
    if case.factory == "bpf":
        files = bpf_files("")
    elif case.factory == "llvm":
        files = llvm_bc_files("", {})
        files["build/platform/python/ymake_python3/ya.make"] = (
            "RESOURCES_LIBRARY()\nNO_YMAKE_PYTHON3()\n"
            "DECLARE_EXTERNAL_RESOURCE(YMAKE_PYTHON3 sbr:2)\nEND()\n"
        )
    elif case.factory == "bison":
        files = bison_files("", {"mod/input.y": "%%\n"})
    elif case.factory == "fortran":
        files = fortran_files({})
        files["contrib/libs/llvm/flang-rt/ya.make"] = (
            f"LIBRARY()\n{BARE}VERSION(1)\nLICENSE(MIT)\n"
            "LICENSE_TEXTS(LICENSE)\nSRCS(runtime.cpp)\nEND()\n"
        )
        files["contrib/libs/llvm/flang-rt/LICENSE"] = "Fixture runtime license\n"
    else:
        files = {}
    if case.factory == "enum":
        files = enum_files(files)
    if case.factory == "alice_enum":
        lib.tool_program(files, "yandex_io/tools/enum_serializer_generator", "enum_serializer_generator")
        files["yandex_io/tools/enum_serializer_generator/linters.make.inc"] = ""
        files["yandex_io/linters.make.inc"] = ""
        files["mod/use.cpp"] = '#include "mod/input.h_serialized.h"\n'
    if case.factory == "tool_sources":
        for path, name in (
            ("contrib/tools/flex-old", "flex"), ("contrib/tools/gperf", "gperf"),
            ("contrib/tools/ragel5/ragel", "ragel5"), ("contrib/tools/ragel5/rlgen-cd", "rlgen-cd"),
            ("contrib/tools/ragel6", "ragel6"), ("tools/html2cpp", "html2cpp"),
            ("tools/domschemec", "domschemec"), ("tools/calcstaticopt", "calcstaticopt"),
            ("tools/relev_fml_codegen", "relev_fml_codegen"),
        ):
            lib.tool_program(files, path, name)
        files["util/system/compiler.h"] = ""
        files["library/cpp/domscheme/ya.make"] = f"LIBRARY()\n{BARE}END()\n"
        files["library/cpp/domscheme/runtime.h"] = ""
    if case.factory in ("resource", "bpf", "llvm", "alice_enum", "pch", "tool_sources"):
        files["library/cpp/resource/ya.make"] = f"LIBRARY()\n{BARE}END()\n"
        for name in ("rescompiler", "rescompressor"):
            lib.tool_program(files, "tools/" + name, name)
    if case.factory == "pch":
        files["build/platform/clang/ya.make"] = lib.resource_lib("CLANG20", 1)
        lib.tool_program(files, "sdg/tier0/tools/make_pch_wrapper", "make_pch_wrapper")
    if case.factory == "mn":
        files["kernel/matrixnet/mn_sse.h"] = ""
    for path, name in (("tools/archiver", "archiver"), ("tools/codegen", "codegen"),
                       ("tools/fix_elf", "fix_elf"),
                       ("contrib/tools/yasm", "yasm")):
        lib.tool_program(files, path, name)
    source = case.source_path or "mod/" + case.source
    files.update({
        "mod/ya.make": (
            f"{case.module}\n{BARE}SET(DONT_RESOLVE_INCLUDES yes)\n"
            + ("USE_LLVM_BC20()\n" if case.factory == "llvm" else "")
            + case.body + "\nEND()\n"
        ),
        source: case.source_content or '#include "mod/' + case.leaf + '"\n',
        "mod/" + case.leaf: "",
        "mod/script.py": "pass\n",
        "later/ya.make": (
            f"{case.later_module}\n{BARE}ADDINCL(mod)\nSRCS(owner.cpp)\nEND()\n"
        ),
        "later/owner.cpp": '#include "' + source + '"\n',
    })
    lib.tool_program(files, "tools/reader", "reader")
    return files


class LateEmitInputsTest(unittest.TestCase):
    def test_sc_input_producer_requires_host_tool_before_export(self):
        for consumers in (1, 2):
            with self.subTest(consumers=consumers):
                self.check_sc_input_producer(consumers)

    def check_sc_input_producer(self, consumers):
        case = next(case for case in CASES if case.name == "sc")
        files = fixture(case)
        files["mod/ya.make"] = files["mod/ya.make"].replace(
            "COPY_FILE_WITH_CONTEXT(input.h.in prepared.sc)",
            "RUN_PROGRAM(tools/reader IN input.h.in OUT_NOAUTO prepared.sc)",
        )
        if consumers == 2:
            files["mod/ya.make"] = files["mod/ya.make"].replace(
                "RESOURCE(${BINDIR}/prepared.sc.h payload)",
                "RESOURCE(${BINDIR}/prepared.sc.h payload)\n"
                "RESOURCE(${BINDIR}/prepared.sc.h second_payload)",
            )
        graph = lib.make(files, "later", "mod", "--add-modules-to-results",
                         "--target-platform", "default-linux-x86_64")
        owner = lib.node_by_output(graph, "$(B)/later/owner.cpp.o")
        self.assertIn("$(S)/mod/leaf.h", owner["inputs"])
        reader = lib.node_by_output(graph, "$(B)/tools/reader/reader")
        host = lib.node_by_output(graph, "$(B)/tools/reader/main.cpp.pic.o")
        self.assertIn(host["uid"], reader["deps"])
        self.assertFalse(any("$(B)/tools/reader/main.cpp.o" in node["outputs"]
                             for node in graph["graph"]))
        prepared = lib.node_by_output(graph, "$(B)/mod/prepared.sc")
        self.assertCountEqual(prepared["inputs"], [
            "$(B)/tools/reader/reader", "$(S)/mod/input.h.in", "$(S)/mod/leaf.h"])
        self.assertEqual(prepared["foreign_deps"]["tool"], [reader["uid"]])
        self.assertIn(reader["uid"], prepared["deps"])
        header = lib.node_by_output(graph, "$(B)/mod/prepared.sc.h")
        self.assertCountEqual(header["inputs"], [
            "$(B)/tools/domschemec/domschemec", "$(B)/mod/prepared.sc",
            "$(S)/mod/input.h.in", "$(S)/mod/leaf.h"])
        self.assertIn(prepared["uid"], header["deps"])
        resources = [node for node in graph["graph"]
                     if any(output.startswith("$(B)/mod/objcopy_")
                            for output in node["outputs"])]
        self.assertEqual(len(resources), consumers)
        for resource in resources:
            self.assertIn(header["uid"], resource["deps"])
            self.assertIn("$(B)/mod/prepared.sc.h", resource["inputs"])
        ids = {node["uid"] for node in graph["graph"]}
        self.assertLessEqual(set(graph["result"]), ids)
        self.assertTrue(all(set(node["deps"]) <= ids for node in graph["graph"]))

    def check(self, case):
        # lib.make appends its target after the extra arguments: mod is first.
        graph = lib.make(fixture(case), "later", "mod",
                         "--target-platform", "default-linux-x86_64")
        owner = lib.node_by_output(graph, "$(B)/later/" + case.owner_output)
        leaf = "$(S)/mod/" + case.leaf
        self.assertIn(leaf, owner["inputs"], "later root did not establish the file binding")
        output = "$(B)/mod/" + case.output
        nodes = [
            node for node in graph["graph"]
            if not node["outputs"][0].endswith(".pkg.fake")
            and any(value.startswith(output) if case.output.endswith("_") else value == output
                    for value in node["outputs"])
        ]
        self.assertEqual(len(nodes), 1, f"expected one producer of {output}")
        node = nodes[0]
        self.assertIn(leaf, node["inputs"], f"{case.emit_site}: earlier command lost the established file binding")


def test_case(case):
    def test(self):
        self.check(case)
    test.__doc__ = case.emit_site
    return test


for case in CASES:
    setattr(LateEmitInputsTest, "test_" + case.name, test_case(case))


if __name__ == "__main__":
    unittest.main(verbosity=2)
