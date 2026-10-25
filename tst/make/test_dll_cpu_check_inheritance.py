from tst import lib
from tst.make.test_yamake_defaults import DEFAULT_PEERS

CASES = {
    "program-source-default-fake": ("SET(DEFAULT_ALLOCATOR FAKE)\nPROGRAM", "", []),
    "program-default": ("PROGRAM", "", []),
    "program-system": ("PROGRAM", "ALLOCATOR(SYSTEM)\n", []),
    "program-lf": ("PROGRAM", "ALLOCATOR(LF)\n", []),
    "program-fake": ("PROGRAM", "ALLOCATOR(FAKE)\n", []),
    "dll-default": ("DLL", "", []),
    "dll-system": ("DLL", "ALLOCATOR(SYSTEM)\n", []),
    "dll-lf": ("DLL", "ALLOCATOR(LF)\n", []),
    "dll-fake": ("DLL", "ALLOCATOR(FAKE)\n", []),
    "udf-default": ("UDF_BASE", "", []),
    "udf-system": ("UDF_BASE", "ALLOCATOR(SYSTEM)\n", []),
    "udf-lf": ("UDF_BASE", "ALLOCATOR(LF)\n", []),
    "program-sse-no": ("PROGRAM", "ALLOCATOR(SYSTEM)\n", ["-DUSE_SSE4=no"]),
    "program-cpu-no": ("PROGRAM", "ALLOCATOR(SYSTEM)\n", ["-DCPU_CHECK=no"]),
    "program-source-cpu-no": ("PROGRAM", "ALLOCATOR(SYSTEM)\nSET(CPU_CHECK no)\n", []),
    "program-source-cpu-yes": ("PROGRAM", "ALLOCATOR(SYSTEM)\nSET(CPU_CHECK yes)\n", []),
    "program-no-util": ("PROGRAM", "ALLOCATOR(SYSTEM)\nNO_UTIL()\n", []),
    "program-arm-default": ("PROGRAM", "ALLOCATOR(SYSTEM)\n", ["--target-platform", "default-linux-aarch64"]),
    "program-arm-sse-yes": ("PROGRAM", "ALLOCATOR(SYSTEM)\n", ["--target-platform", "default-linux-aarch64", "-DUSE_SSE4=yes"]),
    "program-arm-source-sse": ("PROGRAM", "ALLOCATOR(SYSTEM)\nSET(USE_SSE4 yes)\n", ["--target-platform", "default-linux-aarch64"]),
    "program-arm-source-both": ("PROGRAM", "ALLOCATOR(SYSTEM)\nSET(USE_SSE4 yes)\nSET(CPU_CHECK yes)\n", ["--target-platform", "default-linux-aarch64"]),
    "program-instructions-no": ("PROGRAM", "ALLOCATOR(SYSTEM)\n", ["-DDISABLE_INSTRUCTION_SETS=yes"]),
    "program-nosse": ("PROGRAM", "ALLOCATOR(SYSTEM)\n", ["-DNOSSE=yes"]),
    "dll-explicit-peer": ("DLL", "ALLOCATOR(SYSTEM)\nPEERDIR(library/cpp/cpuid_check)\n", []),
    "program-nosse-source-both": ("PROGRAM", "ALLOCATOR(SYSTEM)\nSET(USE_SSE4 yes)\nSET(CPU_CHECK yes)\n", ["-DNOSSE=yes"]),
    "program-instructions-source-both": ("PROGRAM", "ALLOCATOR(SYSTEM)\nSET(USE_SSE4 yes)\nSET(CPU_CHECK yes)\n", ["-DDISABLE_INSTRUCTION_SETS=yes"]),
    "program-source-cpu-sse-no": ("PROGRAM", "ALLOCATOR(SYSTEM)\nSET(CPU_CHECK yes)\n", ["-DUSE_SSE4=no"]),
    "program-fake-source-cpu": ("PROGRAM", "ALLOCATOR(FAKE)\nSET(CPU_CHECK yes)\n", []),
    "program-noutil-source-cpu": ("PROGRAM", "ALLOCATOR(SYSTEM)\nNO_UTIL()\nSET(CPU_CHECK yes)\n", []),
    "program-cpu-then-fake": ("PROGRAM", "SET(CPU_CHECK yes)\nALLOCATOR(FAKE)\nIF (CPU_CHECK == \"yes\")\nCFLAGS(-DCPU_CHECK_VISIBLE)\nENDIF()\n", []),
    "program-fake-then-system": ("PROGRAM", "ALLOCATOR(FAKE)\nALLOCATOR(SYSTEM)\n", []),
    "program-cpu-then-sse": ("PROGRAM", "ALLOCATOR(SYSTEM)\nSET(CPU_CHECK yes)\nSET(USE_SSE4 no)\n", []),
    "program-noutil-scalar": ("PROGRAM", "ALLOCATOR(SYSTEM)\nNO_UTIL()\nSET(NOUTIL yes)\n", []),
    "dll-cpu-yes": ("DLL", "ALLOCATOR(SYSTEM)\nSET(CPU_CHECK yes)\n", []),
}

def fixture(mode):
    macro,body,flags=CASES[mode]
    files={"grove/unit/ya.make":macro+"(cedar)\n"+body+"SRCS(body.cpp)\nEND()\n", "grove/unit/body.cpp":'#include "seed.h"\nint main(){return VALUE;}\n', "grove/unit/seed.h":"#define VALUE 7\n"}
    boundary="NO_PLATFORM()\nNO_UTIL()\nNO_RUNTIME()\nDISABLE(NEED_PLATFORM_PEERDIRS)\n"
    for peer in DEFAULT_PEERS+['library/cpp/malloc/system','library/cpp/lfalloc','contrib/libs/libeatmydata','contrib/libs/cxxsupp']:
        if peer.startswith('build/platform/'):continue
        files[peer+'/ya.make']='LIBRARY()\n'+boundary+'SRCS(piece.cpp)\nEND()\n'
        files[peer+'/piece.cpp']='int boundary_value;\n'
    files['library/cpp/cpuid_check/ya.make']='LIBRARY()\n'+boundary+'SRCS(GLOBAL guard.cpp)\nEND()\n'
    files['library/cpp/cpuid_check/guard.cpp']='int cpu_guard;\n'
    for name in ('fix_elf','archiver'):lib.tool_program(files,'tools/'+name,name)
    return files,'grove/unit',flags

import unittest


def link_node(graph):
    nodes = [n for n in graph["graph"] if n.get("kv", {}).get("p") == "LD"
             and any(o.startswith("$(B)/grove/unit/") for o in n["outputs"])]
    assert len(nodes) == 1
    return nodes[0]


class DllCpuCheckInheritanceTest(unittest.TestCase):
    def test_cpu_check_belongs_to_program_ancestor_and_respects_conditions(self):
        enabled = {"program-default", "program-system", "program-lf", "program-source-cpu-yes", "program-arm-source-both", "dll-explicit-peer", "program-nosse-source-both", "program-instructions-source-both", "program-source-cpu-sse-no", "program-fake-source-cpu", "program-noutil-source-cpu", "program-fake-then-system"}
        for mode in CASES:
            with self.subTest(mode=mode):
                files, target, flags = fixture(mode)
                graph = lib.make(files, target, "--target-platform", "default-linux-x86_64", *flags)
                link = link_node(graph)
                self.assertEqual(any("/util/" in i for i in link["inputs"]), mode not in ("program-no-util", "program-noutil-source-cpu", "program-noutil-scalar"))
                selected = [n for n in graph["graph"] if any("/library/cpp/cpuid_check/" in o for o in n["outputs"])]
                self.assertEqual(bool(selected), mode in enabled)
                if mode in enabled:
                    archive = lib.node_by_output(graph, "$(B)/library/cpp/cpuid_check/liblibrary-cpp-cpuid_check.global.a")
                    self.assertIn(archive["uid"], link["deps"])
                    self.assertIn(archive["outputs"][0], link["inputs"])
                compiled = next(n for n in graph["graph"] if "$(S)/grove/unit/body.cpp" in n["inputs"])
                self.assertIn("$(S)/grove/unit/seed.h", compiled["inputs"])
                if mode == "program-cpu-then-fake":
                    self.assertNotIn("-DCPU_CHECK_VISIBLE", [a for c in compiled["cmds"] for a in c["cmd_args"]])

    def test_go_headers_do_not_inherit_the_program_cpu_scalar(self):
        from tst.make.test_go_tests import go_test_world, go_world

        for macro in ("GO_PROGRAM", "GO_DLL", "GO_TEST"):
            with self.subTest(macro=macro):
                files = go_test_world() if macro == "GO_TEST" else go_world()
                files["grove/go/ya.make"] = (macro + "(cedar)\n"
                    'IF (CPU_CHECK != "")\nMESSAGE(FATAL_ERROR unexpected_cpu_check)\nENDIF()\n'
                    "SRCS(main.go)\nEND()\n")
                files["grove/go/main.go"] = "package main\nfunc main() {}\n"
                graph = lib.make(files, "grove/go", "--target-platform", "default-linux-x86_64")
                self.assertTrue(graph["result"])

    def test_explicit_cpu_peer_is_preserved_for_a_dll(self):
        files, target, flags = fixture("dll-system")
        files[target+"/ya.make"] = files[target+"/ya.make"].replace("END()", "PEERDIR(library/cpp/cpuid_check)\nEND()")
        graph = lib.make(files, target, "--target-platform", "default-linux-x86_64", *flags)
        archive = lib.node_by_output(graph, "$(B)/library/cpp/cpuid_check/liblibrary-cpp-cpuid_check.global.a")
        self.assertIn(archive["uid"], link_node(graph)["deps"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
