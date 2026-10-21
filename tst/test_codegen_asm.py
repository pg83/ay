import unittest

import lib


LIBRARY_HEAD = "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
X86_64 = ("--target-platform", "default-linux-x86_64")
YASM_HEAD = [
    "$(B)/contrib/tools/yasm/yasm", "-f", "elf64", "-D", "UNIX",
    "--replace=$(B)=/-B", "--replace=$(S)=/-S", "--replace=$(TOOL_ROOT)=/-T",
]
YASM_ENV = {"ARCADIA_ROOT_DISTBUILD": "$(S)", "YASM_TEST_SUITE": "1"}


def with_yasm(files):
    lib.tool_program(files, "contrib/tools/yasm", "yasm")
    return files


class YasmTest(unittest.TestCase):
    def test_use_nasm_switches_only_asm_sources_and_uses_the_resource_tool(self):
        for selection in ("USE_NASM()", "ENABLE(USE_NASM_ASSEMBLER)\nPEERDIR(build/external_resources/nasm)"):
            with self.subTest(selection=selection):
                files = with_yasm({
                    "mod/ya.make": (
                        LIBRARY_HEAD + selection + "\nPEERDIR(build/platform/clang)\nSET(NASM_FLAGS -D MODULE=1)\n"
                        "ADDINCL(FOR asm mod/inc)\nSRC(a.asm -D SOURCE=1)\n"
                        "SRCS(forced.yasm plain.S)\nEND()\n"
                    ),
                    "build/platform/clang/ya.make": (
                        "RESOURCES_LIBRARY()\nDECLARE_EXTERNAL_RESOURCE(CLANG20 sbr:987654321)\nEND()\n"
                    ),
                    "mod/a.asm": '%include "defs.inc"\n',
                    "mod/inc/defs.inc": "",
                    "mod/forced.yasm": "",
                    "mod/plain.S": "",
                    "build/external_resources/nasm/ya.make": (
                        "RESOURCES_LIBRARY()\nDECLARE_EXTERNAL_RESOURCE(NASM sbr:123456789)\nEND()\n"
                    ),
                })
                graph = lib.make(files, "mod", *X86_64)
                nasm = lib.node_by_output(graph, "$(B)/mod/a.o")
                self.assertEqual(nasm["cmds"][0]["cmd_args"], [
                    "$(B)/resources/NASM/bin/nasm", "-f", "elf64", "-D", "_x86_64_",
                    "-D", "MODULE=1", "-I", "$(B)", "-I", "$(S)", "-I", "$(S)/mod/inc",
                    "-D", "SOURCE=1", "-o", "$(B)/mod/a.o", "$(S)/mod/a.asm",
                ])
                self.assertEqual(nasm["cmds"][0]["env"], {"ARCADIA_ROOT_DISTBUILD": "$(S)"})
                self.assertCountEqual(nasm["inputs"], ["$(S)/mod/a.asm", "$(S)/mod/inc/defs.inc"])
                self.assertNotIn("foreign_deps", nasm)
                fetched = lib.node_by_output(graph, "$(B)/resources/NASM")
                self.assertIn(fetched["uid"], nasm["deps"])
                yasm = lib.node_by_output(graph, "$(B)/mod/forced.o")
                self.assertEqual(yasm["cmds"][0]["cmd_args"][:len(YASM_HEAD)], YASM_HEAD)
                self.assertNotIn("MODULE=1", yasm["cmds"][0]["cmd_args"])
                self.assertIn("$(B)/contrib/tools/yasm/yasm", yasm["inputs"])
                plain = lib.node_by_output(graph, "$(B)/mod/plain.S.o")
                self.assertTrue(any(arg.endswith("/bin/clang") for arg in plain["cmds"][0]["cmd_args"]))

    def test_nasm_object_format_follows_the_target_platform(self):
        files = {
            "mod/ya.make": LIBRARY_HEAD + "USE_NASM()\nSRCS(a.asm)\nEND()\n",
            "mod/a.asm": "",
            "build/external_resources/nasm/ya.make": (
                "RESOURCES_LIBRARY()\nDECLARE_EXTERNAL_RESOURCE(NASM sbr:123456789)\nEND()\n"
            ),
        }
        for platform, format_ in (("default-darwin-x86_64", "macho64"), ("default-windows-x86_64", "win64")):
            with self.subTest(platform=platform):
                node = lib.node_by_output(lib.make(files, "mod", "--target-platform", platform), "$(B)/mod/a.o")
                self.assertEqual(node["cmds"][0]["cmd_args"][:5], [
                    "$(B)/resources/NASM/bin/nasm", "-f", format_, "-D", "_x86_64_",
                ])

    def test_disabling_nasm_after_use_nasm_restores_yasm(self):
        graph = lib.make(with_yasm({
            "mod/ya.make": LIBRARY_HEAD + "USE_NASM()\nDISABLE(USE_NASM_ASSEMBLER)\nSRCS(a.asm)\nEND()\n",
            "mod/a.asm": "",
            "build/external_resources/nasm/ya.make": "RESOURCES_LIBRARY()\nEND()\n",
        }), "mod", *X86_64)
        node = lib.node_by_output(graph, "$(B)/mod/a.o")
        self.assertEqual(node["cmds"][0]["cmd_args"][:len(YASM_HEAD)], YASM_HEAD)

    def test_yasm_sources_parse_includes_and_honour_asm_addincl(self):
        graph = lib.make(with_yasm({
            "mod/ya.make": (
                LIBRARY_HEAD
                + "ADDINCL(FOR asm mod/inc)\n"
                + "SRCS(a.asm sub/b.asm)\n"
                + "END()\n"
            ),
            "mod/a.asm": (
                '%include "defs.asm"\n'
                "  %  INCLUDE <mod/sys.asm>\n"
                '%include ""x\n'
                '%include "open\n'
                "%inc\n"
                "%define x\n"
                "%include x\n"
                "%includes\n"
                "mov eax, 1\n"
            ),
            "mod/inc/defs.asm": "",
            "mod/sys.asm": "",
            "mod/sub/b.asm": "",
        }), "mod", *X86_64)

        node = lib.node_by_output(graph, "$(B)/mod/a.o")
        self.assertEqual(node["kv"], {"p": "AS", "pc": "light-green"})
        self.assertEqual(node["cmds"][0]["cmd_args"], YASM_HEAD + [
            "-D", "_x86_64_", "-D_YASM_", "-g", "dwarf2",
            "-I", "$(B)", "-I", "$(S)", "-I", "$(S)/mod/inc",
            "-o", "$(B)/mod/a.o", "$(S)/mod/a.asm",
        ])
        self.assertEqual(node["cmds"][0]["env"], YASM_ENV)
        self.assertEqual(lib.bucketed(node["inputs"], 2), lib.bucketed([
            "$(B)/contrib/tools/yasm/yasm",
            "$(S)/mod/a.asm",
            "$(S)/mod/inc/defs.asm",
            "$(S)/mod/sys.asm",
        ], 2))
        yasm = lib.node_by_output(graph, "$(B)/contrib/tools/yasm/yasm")
        self.assertEqual(node["foreign_deps"], {"tool": [yasm["uid"]]})

        nested = lib.node_by_output(graph, "$(B)/mod/_/sub/b.o")
        self.assertEqual(nested["cmds"][0]["cmd_args"][-3:], [
            "-o", "$(B)/mod/_/sub/b.o", "$(S)/mod/sub/b.asm",
        ])
        archive = lib.node_by_output(graph, "$(B)/mod/libmod.a")
        self.assertIn("$(B)/mod/a.o", archive["inputs"])
        self.assertIn("$(B)/mod/_/sub/b.o", archive["inputs"])

    def test_c_and_yasm_include_directories_remain_separate(self):
        graph = lib.make(with_yasm({
            "mod/ya.make": (
                LIBRARY_HEAD + "ADDINCL(c_include FOR asm asm_include)\n"
                "SRCS(routine.asm routine.S)\nEND()\n"
            ),
            "mod/routine.asm": '%include "defs.inc"\n',
            "mod/routine.S": '#include "defs.h"\n',
            "c_include/defs.inc": "",
            "c_include/defs.h": "",
            "asm_include/defs.inc": "",
            "asm_include/defs.h": "",
        }), "mod", *X86_64)
        yasm = lib.node_by_output(graph, "$(B)/mod/routine.o")
        assembler = lib.node_by_output(graph, "$(B)/mod/routine.S.o")
        self.assertIn("$(S)/asm_include", yasm["cmds"][0]["cmd_args"])
        self.assertNotIn("$(S)/c_include", yasm["cmds"][0]["cmd_args"])
        self.assertIn("-I$(S)/c_include", assembler["cmds"][0]["cmd_args"])
        self.assertNotIn("-I$(S)/asm_include", assembler["cmds"][0]["cmd_args"])
        self.assertIn("$(S)/asm_include/defs.inc", yasm["inputs"])
        self.assertNotIn("$(S)/c_include/defs.inc", yasm["inputs"])
        self.assertIn("$(S)/c_include/defs.h", assembler["inputs"])
        self.assertNotIn("$(S)/asm_include/defs.h", assembler["inputs"])

    def test_asmlib_module_omits_debug_info(self):
        graph = lib.make(with_yasm({
            "contrib/libs/asmlib/ya.make": LIBRARY_HEAD + "SRCS(m.asm)\nEND()\n",
            "contrib/libs/asmlib/m.asm": "",
        }), "contrib/libs/asmlib", *X86_64)
        node = lib.node_by_output(graph, "$(B)/contrib/libs/asmlib/m.o")
        self.assertNotIn("dwarf2", node["cmds"][0]["cmd_args"])
        self.assertEqual(node["cmds"][0]["cmd_args"][-7:], [
            "-I", "$(B)", "-I", "$(S)",
            "-o", "$(B)/contrib/libs/asmlib/m.o", "$(S)/contrib/libs/asmlib/m.asm",
        ])

    def test_host_tool_yasm_object_is_pic(self):
        files = with_yasm({
            "mod/ya.make": (
                LIBRARY_HEAD + "SRCS(use.cpp)\nARCHIVE(NAME data.inc payload.txt)\nEND()\n"
            ),
            "mod/use.cpp": '#include "data.inc"\n',
            "mod/payload.txt": "payload\n",
            "tools/archiver/ya.make": (
                "PROGRAM(archiver)\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
                "SRCS(main.cpp fast.asm)\nEND()\n"
            ),
            "tools/archiver/main.cpp": "int main(){return 0;}\n",
            "tools/archiver/fast.asm": "",
        })
        graph = lib.make(files, "mod", *X86_64)
        node = lib.node_by_output(graph, "$(B)/tools/archiver/fast.pic.o")
        self.assertEqual(node["cmds"][0]["cmd_args"][-3:], [
            "-o", "$(B)/tools/archiver/fast.pic.o", "$(S)/tools/archiver/fast.asm",
        ])


class AssemblerTest(unittest.TestCase):
    def test_gnu_assembler_sources_compile_with_clang(self):
        graph = lib.make({
            "mod/ya.make": (
                LIBRARY_HEAD
                + "ADDINCL(FOR asm mod/inc)\n"
                + "SRCS(c.S sub/d.s)\n"
                + "END()\n"
            ),
            "mod/inc/.keep": "",
            "mod/c.S": '#include "hdr.h"\n',
            "mod/hdr.h": "",
            "mod/sub/d.s": "",
        }, "mod", *X86_64)

        node = lib.node_by_output(graph, "$(B)/mod/c.S.o")
        self.assertEqual(node["kv"], {"p": "AS", "pc": "light-green"})
        args = node["cmds"][0]["cmd_args"]
        self.assertEqual(args[1], "--target=x86_64-linux-gnu")
        self.assertEqual(args[-6:], [
            "-c", "-o", "$(B)/mod/c.S.o", "$(S)/mod/c.S",
            "-I$(B)", "-I$(S)",
        ])
        self.assertIn("-DCATBOOST_OPENSOURCE=yes", args)
        self.assertEqual(node["cmds"][0]["cwd"], "$(B)")
        self.assertEqual(node["inputs"], ["$(S)/mod/c.S", "$(S)/mod/hdr.h"])

        nested = lib.node_by_output(graph, "$(B)/mod/_/sub/d.s.o")
        nested_args = nested["cmds"][0]["cmd_args"]
        start = nested_args.index("-c")
        self.assertEqual(nested_args[start:start + 4], [
            "-c", "-o", "$(B)/mod/_/sub/d.s.o", "$(S)/mod/sub/d.s",
        ])

    def test_srcdir_sources_use_relative_output_paths(self):
        graph = lib.make(with_yasm({
            "mod/ya.make": (
                LIBRARY_HEAD + "SRCDIR(other/dir)\nSRCS(x.S y.asm)\nEND()\n"
            ),
            "other/dir/x.S": "",
            "other/dir/y.asm": "",
        }), "mod", *X86_64)
        gnu = lib.node_by_output(graph, "$(B)/mod/__/other/dir/x.S.o")
        self.assertEqual(gnu["inputs"], ["$(S)/other/dir/x.S"])
        self.assertIn("$(S)/other/dir/x.S", gnu["cmds"][0]["cmd_args"])
        yasm = lib.node_by_output(graph, "$(B)/mod/__/other/dir/y.o")
        self.assertEqual(yasm["cmds"][0]["cmd_args"][-1], "$(S)/other/dir/y.asm")

    def test_go_module_assembler_forces_consistent_debug_paths(self):
        empty = "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nEND()\n"
        graph = lib.make({
            "gomod/ya.make": "GO_LIBRARY()\nSRCS(a.go x.S)\nEND()\n",
            "gomod/a.go": "package gomod\n",
            "gomod/x.S": "",
            "build/external_resources/go_tools/ya.make": empty,
            "build/external_resources/yolint/ya.make": empty,
        }, "gomod", *X86_64)
        node = lib.node_by_output(graph, "$(B)/gomod/x.S.o")
        args = node["cmds"][0]["cmd_args"]
        self.assertEqual(args[1:4], ["--target=x86_64-linux-gnu", "-B/usr/bin", "-fdebug-prefix-map=$(B)=/-B"])
        self.assertEqual(args.count("-fdebug-prefix-map=$(B)=/-B"), 2)
        self.assertEqual(args.count("-fdebug-compilation-dir"), 2)
        start = args.index("-c")
        self.assertEqual(args[start:start + 6], [
            "-c", "-o", "$(B)/gomod/x.S.o", "$(S)/gomod/x.S", "-I$(B)", "-I$(S)",
        ])


class ArchiveAsmTest(unittest.TestCase):
    def files(self, archive):
        files = with_yasm({
            "mod/ya.make": LIBRARY_HEAD + archive + "END()\n",
            "mod/x.bin": "x",
            "mod/y.bin": "y",
            "build/scripts/rodata2asm.py": "",
        })
        lib.tool_program(files, "tools/archiver", "archiver")
        return files

    def test_archive_asm_builds_rodata_object(self):
        graph = lib.make(
            self.files("ARCHIVE_ASM(NAME blob DONTCOMPRESS x.bin y.bin x.bin)\n"),
            "mod", *X86_64,
        )
        archiver = lib.node_by_output(graph, "$(B)/tools/archiver/archiver")
        archive = lib.node_by_output(graph, "$(B)/mod/blob.rodata")
        self.assertEqual(archive["kv"], {"p": "AR", "pc": "light-cyan"})
        self.assertEqual(archive["cmds"][0]["cmd_args"], [
            "$(B)/tools/archiver/archiver", "-q", "-p",
            "$(S)/mod/x.bin:", "$(S)/mod/y.bin:", "$(S)/mod/x.bin:",
            "-o", "$(B)/mod/blob.rodata",
        ])
        self.assertEqual(archive["inputs"], [
            "$(S)/mod/x.bin", "$(S)/mod/y.bin", "$(B)/tools/archiver/archiver",
        ])
        self.assertEqual(archive["deps"], [archiver["uid"]])

        rodata = lib.node_by_output(graph, "$(B)/mod/blob.rodata.o")
        self.assertEqual(rodata["kv"]["p"], "RD")
        self.assertEqual(rodata["outputs"], [
            "$(B)/mod/blob.rodata.asm", "$(B)/mod/blob.rodata.o",
        ])
        self.assertEqual(rodata["cmds"][0]["cmd_args"][1:], [
            "$(S)/build/scripts/rodata2asm.py", "--elf", "blob",
            "$(B)/mod/blob.rodata", "$(B)/mod/blob.rodata.asm",
        ])
        self.assertEqual(rodata["cmds"][1]["cmd_args"][-3:], [
            "-o", "$(B)/mod/blob.rodata.o", "$(B)/mod/blob.rodata.asm",
        ])
        self.assertEqual(lib.bucketed(rodata["inputs"], 3), [
            '$(B)/contrib/tools/yasm/yasm',
            '$(S)/build/scripts/rodata2asm.py',
            '$(B)/mod/blob.rodata',
        ])
        self.assertIn(archive["uid"], rodata["deps"])
        library = lib.node_by_output(graph, "$(B)/mod/libmod.a")
        self.assertIn("$(B)/mod/blob.rodata.o", library["inputs"])
        self.assertIn("$(B)/mod/blob.rodata.asm", library["inputs"])
        self.assertIn(rodata["uid"], library["deps"])

    def test_global_rodata_archive_keeps_its_producer_main_output(self):
        files = self.files("SRCS(GLOBAL data.rodata)\n")
        files["mod/data.rodata"] = "binary"
        graph = lib.make(files, "mod", *X86_64)
        rodata = lib.node_by_output(graph, "$(B)/mod/data.rodata.o")
        archive = lib.node_by_output(graph, "$(B)/mod/libmod.global.a")
        self.assertIn("$(B)/mod/data.rodata.asm", archive["inputs"])
        self.assertIn(rodata["uid"], archive["deps"])

    def test_archive_asm_compresses_by_default(self):
        graph = lib.make(self.files("ARCHIVE_ASM(NAME blob x.bin)\n"), "mod", *X86_64)
        archive = lib.node_by_output(graph, "$(B)/mod/blob.rodata")
        self.assertEqual(archive["cmds"][0]["cmd_args"][1:], [
            "-q", "$(S)/mod/x.bin:", "-o", "$(B)/mod/blob.rodata",
        ])

    def test_archive_asm_member_produced_by_copy_file(self):
        files = self.files("COPY_FILE(x.bin gen.bin)\nARCHIVE_ASM(NAME blob gen.bin y.bin)\n")
        graph = lib.make(files, "mod", *X86_64)
        copy = lib.node_by_output(graph, "$(B)/mod/gen.bin")
        archive = lib.node_by_output(graph, "$(B)/mod/blob.rodata")
        self.assertEqual(archive["cmds"][0]["cmd_args"][1:], [
            "-q", "$(B)/mod/gen.bin:", "$(S)/mod/y.bin:", "-o", "$(B)/mod/blob.rodata",
        ])
        self.assertEqual(archive["inputs"], [
            "$(B)/mod/gen.bin", "$(S)/mod/y.bin", "$(B)/tools/archiver/archiver",
        ])
        self.assertIn(copy["uid"], archive["deps"])
        rodata = lib.node_by_output(graph, "$(B)/mod/blob.rodata.o")
        self.assertEqual(sorted(rodata["inputs"][3:]), [])

    def test_archive_asm_requires_x86_64_target(self):
        with self.assertRaisesRegex(
            AssertionError,
            'unsupported .rodata platform aarch64 for ARCHIVE_ASM "blob.rodata"',
        ):
            lib.make(self.files("ARCHIVE_ASM(NAME blob x.bin)\n"), "mod")


if __name__ == "__main__":
    unittest.main(verbosity=2)
