import json
import unittest

import lib


STD = "contrib/go/_std_1.26/src"
NO_PLATFORM = "NO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
ZSTD = "vendor/github.com/DataDog/zstd"
CGO_TOOL = "$(B)/resources/GO_TOOLS/pkg/tool/linux_amd64/cgo"
CLANG = "$(B)/resources/CLANG20/bin/clang"


def resources_library(files, path, name):
    bundle = f"{name.lower()}.json"
    files[f"{path}/ya.make"] = (
        f"RESOURCES_LIBRARY()\nDECLARE_EXTERNAL_HOST_RESOURCES_BUNDLE_BY_JSON({name} {bundle})\nEND()\n"
    )
    files[f"{path}/{bundle}"] = json.dumps({"by_platform": {
        platform: {"uri": f"sbr:{len(files)}{index}"}
        for index, platform in enumerate(("linux", "linux-x86_64", "linux-aarch64"))
    }})


def go_module(files, path, body="", module="GO_LIBRARY"):
    name = path.rsplit("/", 1)[-1]
    files[f"{path}/ya.make"] = f"{module}()\nSRCS({name}.go)\nEND()\n"
    files[f"{path}/{name}.go"] = f"package {name}\n{body}"


def cgo_world():
    files = {
        "build/cow/on/ya.make": f"LIBRARY()\n{NO_PLATFORM}END()\n",
        "contrib/libs/linux-headers/ya.make": f"LIBRARY()\n{NO_PLATFORM}END()\n",
        "build/scripts/fs_tools.py": "import os\nimport process_command_files as pcf\n",
        "build/scripts/process_command_files.py": "import sys\n",
        "build/scripts/link_o.py": "import subprocess\n",
        f"{STD}/runtime/textflag.h": "#define NOSPLIT 4\n",
    }
    resources_library(files, "build/external_resources/go_tools", "GO_TOOLS")
    resources_library(files, "build/external_resources/yolint", "YOLINT")
    resources_library(files, "build/platform/lld", "LLD_ROOT")
    resources_library(files, "build/internal/platform/clang_toolchain_info", "CLANG20")
    for path in (f"{STD}/runtime", f"{STD}/runtime/cgo", f"{STD}/syscall", f"{STD}/errors", "library/go/core/buildinfo"):
        go_module(files, path)
    return files


def zstd_binding(files):
    files.update({
        "contrib/libs/zstd/ya.make": (
            f"LIBRARY()\n{NO_PLATFORM}ADDINCL(GLOBAL contrib/libs/zstd/include)\nSRCS(zstd.c)\nEND()\n"
        ),
        "contrib/libs/zstd/zstd.c": "int zstd(void) { return 0; }\n",
        "contrib/libs/zstd/include/zstd.h": "int zstd(void);\n",
        f"{ZSTD}/ya.make": (
            "GO_LIBRARY()\nLICENSE(BSD-3-Clause)\nPEERDIR(contrib/libs/zstd)\n"
            "CGO_CFLAGS(-DZSTD_STATIC_LINKING_ONLY)\nCGO_LDFLAGS(-lm)\n"
            "SRCS(stub.go CGO_EXPORT helper.c CGO_EXPORT bridge.cxx trampoline.S)\n"
            "CGO_SRCS(zstd.go errors.go)\nEND()\n"
        ),
        f"{ZSTD}/stub.go": 'package zstd\n\nimport "errors"\n',
        f"{ZSTD}/helper.c": '#include "helper.h"\n#include "helper_impl.h"\nint helper(void) { return 1; }\n',
        f"{ZSTD}/helper_impl.h": "#define HELPER 1\n",
        f"{ZSTD}/bridge.cxx": '#include "helper.h"\nint bridge() { return 2; }\n',
        f"{ZSTD}/helper.h": "int helper(void);\n",
        f"{ZSTD}/trampoline.S": ".text\n",
        f"{ZSTD}/zstd.go": 'package zstd\n\n/*\n#include "zstd.h"\n#include "helper.h"\n*/\nimport "C"\n',
        f"{ZSTD}/errors.go": 'package zstd\n\n// #include "zstd.h"\nimport "C"\n',
    })


def between(args, start, end):
    begin = args.index(start) + 1
    return args[begin:args.index(end, begin)]


class GoCgoGraphTest(unittest.TestCase):
    def test_cgo_binding_emits_cgo_pipeline_and_links_c_archives(self):
        files = cgo_world()
        zstd_binding(files)
        go_module(files, "cmd/app", 'import "github.com/DataDog/zstd"\n', module="GO_PROGRAM")
        graph = lib.make(files, "cmd/app")
        b = f"$(B)/{ZSTD}"
        s = f"$(S)/{ZSTD}"
        cflags = ["-w", "-pthread", "-fpic", "-DZSTD_STATIC_LINKING_ONLY"]
        includes = [
            "-I$(B)", "-I$(S)", f"-I$(S)/{STD}/runtime", f"-I{s}", "-I$(S)/contrib/libs/zstd/include",
        ]

        cgo1 = lib.node_by_output(graph, f"{b}/_cgo_gotypes.go")
        self.assertEqual(cgo1["kv"]["p"], "go")
        self.assertEqual(cgo1["outputs"], [
            f"{b}/zstd.cgo1.go", f"{b}/errors.cgo1.go",
            f"{b}/zstd.cgo2.c", f"{b}/errors.cgo2.c",
            f"{b}/_cgo_export.h", f"{b}/_cgo_export.c", f"{b}/_cgo_gotypes.go", f"{b}/_cgo_main.c",
        ])
        self.assertEqual(cgo1["inputs"], [
            "$(S)/build/scripts/cgo1_wrapper.py", f"{s}/zstd.go", f"{s}/errors.go",
            f"{s}/helper.h", "$(S)/contrib/libs/zstd/include/zstd.h",
        ])
        self.assertEqual(cgo1["cmds"][0]["cwd"], "$(S)")
        self.assertEqual(between(cgo1["cmds"][0]["cmd_args"], "--cgo1-files", "--"), [
            f"{b}/zstd.cgo1.go", f"{b}/errors.cgo1.go",
            "--cgo2-files", f"{b}/zstd.cgo2.c", f"{b}/errors.cgo2.c",
        ])
        self.assertEqual(between(cgo1["cmds"][0]["cmd_args"], CGO_TOOL, f"{s}/zstd.go"), [
            "-objdir", b, "-importpath", "github.com/DataDog/zstd",
            "-import_runtime_cgo=true", "-import_syscall=true",
            "--", "--target=aarch64-linux-gnu", "-B/usr/bin", *includes, *cflags,
        ])

        copy = lib.node_by_output(graph, f"{b}/helper.c")
        self.assertEqual(copy["kv"]["p"], "CP")
        self.assertEqual(copy["cmds"][0]["cmd_args"][1:], [
            "$(S)/build/scripts/fs_tools.py", "copy", f"{s}/helper.c", f"{b}/helper.c",
        ])
        self.assertEqual(copy["inputs"], [
            "$(S)/build/scripts/fs_tools.py", "$(S)/build/scripts/process_command_files.py",
            f"{s}/helper.c", f"{s}/helper.h", f"{s}/helper_impl.h",
            "$(S)/build/scripts/cgo1_wrapper.py", f"{s}/zstd.go", f"{s}/errors.go",
        ])
        helper_cc = lib.node_by_output(graph, f"{b}/helper.c.o")
        self.assertEqual(helper_cc["cmds"][0]["cmd_args"][-5:], [*cflags, f"{b}/helper.c"])
        self.assertIn(f"{b}/_cgo_export.h", helper_cc["inputs"])
        cgo2_cc = lib.node_by_output(graph, f"{b}/zstd.cgo2.c.o")
        self.assertEqual(cgo2_cc["cmds"][0]["cmd_args"][-6:], [*cflags, "-Wno-unused-variable", f"{b}/zstd.cgo2.c"])

        objects = [
            f"{b}/_cgo_export.c.o", f"{b}/zstd.cgo2.c.o", f"{b}/errors.cgo2.c.o",
            f"{b}/helper.c.o", f"{b}/bridge.cxx.o", f"{b}/trampoline.S.o",
        ]
        dynimport = lib.node_by_output(graph, f"{b}/_cgo_import.go")
        self.assertEqual(dynimport["outputs"], [f"{b}/_cgo_main.c.o", f"{b}/_cgo_.o", f"{b}/_cgo_import.go"])
        main_cc, link_o, dyn = (cmd["cmd_args"] for cmd in dynimport["cmds"])
        self.assertEqual(main_cc, [
            CLANG, "--target=aarch64-linux-gnu", "-B/usr/bin", *includes, *cflags,
            f"{b}/_cgo_main.c", "-c", "-o", f"{b}/_cgo_main.c.o",
        ])
        self.assertEqual(link_o[1:3], ["$(S)/build/scripts/link_o.py", CLANG])
        self.assertEqual(between(link_o, "-o", f"{b}/_cgo_main.c.o"), [
            f"{b}/_cgo_.o", "-ldl", "-lrt", "-Wl,--no-as-needed",
            "-fuse-ld=lld", "--ld-path=$(B)/resources/LLD_ROOT/bin/ld.lld",
            "-Wl,--no-rosegment", "-Wl,--build-id=sha1", "-Wl,--unresolved-symbols=ignore-all",
            "-nodefaultlibs", "-lc",
        ])
        self.assertEqual(link_o[link_o.index(f"{b}/_cgo_main.c.o"):], [f"{b}/_cgo_main.c.o", *objects, "-lm"])
        self.assertEqual(dyn, [
            CGO_TOOL, "-dynpackage", "zstd", "-dynimport", f"{b}/_cgo_.o",
            "-dynout", f"{b}/_cgo_import.go", "-dynlinker",
        ])
        self.assertEqual(dynimport["inputs"], [
            "$(S)/build/scripts/link_o.py", "$(S)/build/scripts/cgo1_wrapper.py", "$(S)/build/scripts/wrapcc.py",
            "$(S)/build/scripts/fs_tools.py", "$(S)/build/scripts/process_command_files.py",
            f"{s}/zstd.go", f"{s}/errors.go", f"{s}/helper.h", "$(S)/contrib/libs/zstd/include/zstd.h",
            f"{s}/helper.c", f"{s}/helper_impl.h", f"{s}/bridge.cxx", f"{s}/trampoline.S",
            f"{b}/_cgo_export.h", f"{b}/_cgo_main.c", f"{b}/zstd.cgo1.go", *objects,
        ])
        by_output = {node["outputs"][0]: node["uid"] for node in graph["graph"]}
        self.assertEqual(dynimport["deps"][:len(objects)], [by_output[o] for o in objects])

        package = lib.node_by_output(graph, f"{b}/zstd.a")
        args = next(cmd["cmd_args"] for cmd in package["cmds"])
        self.assertEqual(between(args, "++srcs", "++asm-flags"), [
            f"{b}/helper.c.o", f"{b}/bridge.cxx.o", f"{b}/trampoline.S.o",
            f"{b}/zstd.cgo1.go", f"{b}/errors.cgo1.go", f"{b}/_cgo_gotypes.go",
            f"{b}/zstd.cgo2.c.o", f"{b}/errors.cgo2.c.o", f"{b}/_cgo_export.c.o",
            f"{b}/_cgo_import.go", f"{s}/stub.go",
        ])
        self.assertEqual(between(args, "++cgo-srcs", "++peers"), [f"{s}/zstd.go", f"{s}/errors.go"])
        self.assertEqual(between(args, "++peers", "--ya-end-command-file"), [
            f"{STD}/syscall/syscall.a", f"{STD}/runtime/cgo/cgo.a", f"{STD}/errors/errors.a",
        ])
        self.assertEqual(package["inputs"][-10:], [
            "$(S)/build/scripts/cgo1_wrapper.py", "$(S)/build/scripts/wrapcc.py",
            "$(S)/build/scripts/fs_tools.py", "$(S)/build/scripts/link_o.py",
            f"{s}/helper.c", f"{s}/helper.h", f"{s}/helper_impl.h", f"{s}/bridge.cxx", f"{s}/trampoline.S",
            f"{b}/_cgo_main.c.o",
        ])

        link = lib.node_by_output(graph, "$(B)/cmd/app/app")
        link_args = link["cmds"][-1]["cmd_args"]
        self.assertEqual(between(link_args, "++peers", "++non-local-peers")[-1], f"{ZSTD}/zstd.a")
        self.assertEqual(between(link_args, "++non-local-peers", "++cgo-peers"), [
            f"{STD}/syscall/syscall.a", f"{STD}/errors/errors.a",
        ])
        self.assertEqual(between(link_args, "++cgo-peers", "--ya-end-command-file"), [
            "cmd/app/__vcs_version__.c.o", "contrib/libs/zstd/libcontrib-libs-zstd.a",
        ])

    def test_runtime_cgo_does_not_import_itself(self):
        files = cgo_world()
        cgo = f"{STD}/runtime/cgo"
        files.update({
            f"{cgo}/ya.make": (
                "GO_LIBRARY()\nNO_COMPILER_WARNINGS()\n"
                "SRCS(callbacks.go CGO_EXPORT gcc_linux_amd64.c gcc_amd64.S asm_amd64.s libcgo.h)\n"
                "CGO_LDFLAGS(-lpthread -ldl -lresolv)\nCGO_SRCS(cgo.go)\nEND()\n"
            ),
            f"{cgo}/callbacks.go": 'package cgo\n\nimport "unsafe"\n',
            f"{cgo}/cgo.go": 'package cgo\n\n/*\n#cgo linux LDFLAGS: -lpthread\n#include "libcgo.h"\n*/\nimport "C"\n',
            f"{cgo}/libcgo.h": '#include "textflag.h"\n',
            f"{cgo}/gcc_linux_amd64.c": '#include "libcgo.h"\nvoid x_cgo_init(void) {}\n',
            f"{cgo}/gcc_amd64.S": ".text\n",
            f"{cgo}/asm_amd64.s": '#include "textflag.h"\nTEXT crosscall2(SB),NOSPLIT,$0-0\n',
        })
        graph = lib.make(files, cgo)
        cgo1 = lib.node_by_output(graph, f"$(B)/{cgo}/_cgo_gotypes.go")
        args = cgo1["cmds"][0]["cmd_args"]
        self.assertEqual(between(args, "-importpath", "--"), [
            "runtime/cgo", "-import_runtime_cgo=false", "-import_syscall=false",
        ])
        self.assertEqual(args[-4:], ["-w", "-pthread", "-fpic", f"$(S)/{cgo}/cgo.go"])
        self.assertEqual(cgo1["inputs"], [
            "$(S)/build/scripts/cgo1_wrapper.py", f"$(S)/{cgo}/cgo.go",
            f"$(S)/{STD}/runtime/textflag.h", f"$(S)/{cgo}/libcgo.h",
        ])
        link_o = lib.node_by_output(graph, f"$(B)/{cgo}/_cgo_.o")["cmds"][1]["cmd_args"]
        self.assertEqual(link_o[-3:], ["-lpthread", "-ldl", "-lresolv"])
        package = lib.node_by_output(graph, f"$(B)/{cgo}/cgo.a")
        self.assertEqual(between(package["cmds"][0]["cmd_args"], "++cgo-srcs", "++peers"), [
            f"$(S)/{cgo}/cgo.go",
        ])
        srcs = between(package["cmds"][0]["cmd_args"], "++srcs", "++asm-flags")
        self.assertEqual([srcs[0], srcs[-1]], [f"$(B)/{cgo}/gen.symabis", f"$(S)/{cgo}/asm_amd64.s"])
        tool_inputs_end = package["inputs"].index("$(S)/build/rules/go/risky_imports.yaml") + 1
        self.assertEqual(package["inputs"][tool_inputs_end:tool_inputs_end + 2], [
            f"$(S)/{STD}/runtime/textflag.h", "$(S)/build/scripts/cgo1_wrapper.py",
        ])
        symabis = lib.node_by_output(graph, f"$(B)/{cgo}/gen.symabis")["cmds"][0]["cmd_args"]
        self.assertEqual(between(symabis, "-trimpath", "-D")[1:], [
            "-I", "$(B)", "-I", "$(S)", "-I", f"$(S)/{STD}/runtime",
            "-I", "$(S)/build/scripts/go_fake_include", "-I", f"$(S)/{cgo}",
            "-I", "$(B)/resources/GO_TOOLS/pkg/include",
        ])
        self.assertEqual(between(symabis, "-p", "-o"), ["runtime/cgo", "-gensymabis"])

    def test_cgo_link_object_flags_follow_platform(self):
        files = cgo_world()
        zstd_binding(files)
        files["build/ymake_conf.py"] = "debug_info_flags.append('-gz=zstd')\n"
        go_module(files, "tools/gogen", 'import "github.com/DataDog/zstd"\n', module="GO_PROGRAM")
        files["lib/gen/ya.make"] = (
            f"LIBRARY()\n{NO_PLATFORM}RUN_PROGRAM(tools/gogen out.cpp OUT out.cpp)\nEND()\n"
        )
        for target, flags in (
            (ZSTD, [f"$(B)/{ZSTD}/_cgo_.o", "-Wl,--compress-debug-sections=zstd", "-ldl", "-lrt", "-Wl,--no-as-needed"]),
            ("lib/gen", [f"$(B)/{ZSTD}/_cgo_.pic.o", "-ldl", "-lrt", "-Wl,--no-as-needed", "-fPIC", "-fPIC"]),
        ):
            with self.subTest(target=target):
                graph = lib.make(files, target)
                link_o = lib.node_by_output(graph, f"$(B)/{ZSTD}/_cgo_import.go")["cmds"][1]["cmd_args"]
                self.assertEqual(between(link_o, "-o", "-fuse-ld=lld"), flags)

    def test_generated_cgo_source_is_not_read_for_imports(self):
        files = cgo_world()
        lib.tool_program(files, "contrib/tools/swig", "swig")
        pkg = "library/go/yandex/geobase/internal"
        files.update({
            f"{pkg}/ya.make": (
                "GO_LIBRARY()\nSRCS(stub.go)\nCGO_SRCS(${BINDIR}/internal.go)\n"
                "RUN_PROGRAM(\n    contrib/tools/swig -go -cgo -outdir ${BINDIR} ${MODDIR}/iface.txt\n"
                "    CWD ${ARCADIA_ROOT}\n    IN iface.txt\n    OUT_NOAUTO ${BINDIR}/internal.go\n)\nEND()\n"
            ),
            f"{pkg}/iface.txt": "%module internal\n",
            f"{pkg}/stub.go": "package internal\n",
        })
        graph = lib.make(files, pkg)
        lib.node_by_output(graph, f"$(B)/{pkg}/internal.go")
        cgo1 = lib.node_by_output(graph, f"$(B)/{pkg}/_cgo_gotypes.go")
        self.assertEqual(cgo1["inputs"], ["$(S)/build/scripts/cgo1_wrapper.py", f"$(B)/{pkg}/internal.go"])
        self.assertEqual(cgo1["cmds"][0]["cmd_args"][-1], f"$(B)/{pkg}/internal.go")

    # Defect: cgo output names are derived from the CGO_SRCS token text, so a ${BINDIR} source
    # yields "$(B)/<dir>/$(B)/<dir>/internal.cgo1.go".
    @unittest.expectedFailure
    def test_generated_cgo_source_outputs_stay_in_module_build_dir(self):
        files = cgo_world()
        pkg = "library/go/gen"
        files.update({
            f"{pkg}/ya.make": (
                "GO_LIBRARY()\nSRCS(gen.go)\nCOPY_FILE(template.txt ${BINDIR}/internal.go)\n"
                "CGO_SRCS(${BINDIR}/internal.go)\nEND()\n"
            ),
            f"{pkg}/template.txt": 'package gen\n\nimport "C"\n',
            f"{pkg}/gen.go": "package gen\n",
        })
        graph = lib.make(files, pkg)
        lib.node_by_output(graph, f"$(B)/{pkg}/internal.cgo1.go")

    def test_cgo_includes_come_only_from_go_comments(self):
        files = cgo_world()
        pkg = "library/go/cparse"
        for header in ("wanted_a.h", "wanted_b.h", "wanted_c.h", "table.inc", "ghost_a.h", "ghost_b.h", "ghost_c.h", "ghost_d.h"):
            files[f"{pkg}/{header}"] = f"// {header}\n"
        files[f"{pkg}/ya.make"] = "GO_LIBRARY()\nSRCS(cparse.go)\nCGO_SRCS(preamble.go plain.go doc.go)\nEND()\n"
        files[f"{pkg}/cparse.go"] = "package cparse\n"
        files[f"{pkg}/preamble.go"] = r'''package cparse

/*
#include "wanted_a.h"
#include "table.inc"
*/
import "C"

const url = "http://example.com/*#include \"ghost_a.h\"*/"
const quote = '"'
const apostrophe = '\''
const text = "tab\t// #include \"ghost_b.h\""
const raw = `
#include "ghost_c.h"
/* #include "ghost_d.h" */`

// #include "wanted_b.h"
/* never closed
#include "wanted_c.h"
'''
        files[f"{pkg}/plain.go"] = 'package cparse\n\nimport "C"\n\nvar size C.int\n'
        files[f"{pkg}/doc.go"] = '// Package cparse wraps C declarations.\npackage cparse\n\nimport "C"\n'
        graph = lib.make(files, pkg)
        cgo1 = lib.node_by_output(graph, f"$(B)/{pkg}/_cgo_gotypes.go")
        self.assertEqual(cgo1["inputs"][:4], [
            "$(S)/build/scripts/cgo1_wrapper.py",
            f"$(S)/{pkg}/preamble.go", f"$(S)/{pkg}/plain.go", f"$(S)/{pkg}/doc.go",
        ])
        self.assertCountEqual(cgo1["inputs"][4:], [
            f"$(S)/{pkg}/wanted_a.h", f"$(S)/{pkg}/table.inc", f"$(S)/{pkg}/wanted_b.h", f"$(S)/{pkg}/wanted_c.h",
        ])

    def test_module_with_only_cgo_sources_links_dynimport_object(self):
        files = cgo_world()
        pkg = "library/go/yandex/authparser"
        files.update({
            f"{pkg}/ya.make": "GO_LIBRARY()\nCGO_SRCS(decode.go)\nEND()\n",
            f"{pkg}/decode.go": 'package authparser\n\n// #include "extern_c.h"\nimport "C"\nimport "errors"\n',
            f"{pkg}/extern_c.h": "int GetLocation(void);\n",
        })
        graph = lib.make(files, pkg)
        dynimport = lib.node_by_output(graph, f"$(B)/{pkg}/_cgo_import.go")
        self.assertEqual(dynimport["cmds"][2]["cmd_args"][1:3], ["-dynpackage", "authparser"])
        self.assertIn(f"$(S)/{pkg}/extern_c.h", dynimport["inputs"])
        package = lib.node_by_output(graph, f"$(B)/{pkg}/authparser.a")
        args = package["cmds"][0]["cmd_args"]
        self.assertEqual(between(args, "++srcs", "++asm-flags")[-1], f"$(B)/{pkg}/_cgo_import.go")
        self.assertEqual(between(args, "++cgo-srcs", "++peers"), [f"$(S)/{pkg}/decode.go"])
        self.assertIn(dynimport["uid"], package["deps"])

    # Defect: producerPositions() omits CGO_SRCS from its early-exit count, so a module whose only
    # sources are CGO_SRCS never runs the cgo1 producer while _cgo_.o still links its outputs.
    @unittest.expectedFailure
    def test_module_with_only_cgo_sources_emits_cgo1_node(self):
        files = cgo_world()
        pkg = "library/go/yandex/authparser"
        files.update({
            f"{pkg}/ya.make": "GO_LIBRARY()\nCGO_SRCS(decode.go)\nEND()\n",
            f"{pkg}/decode.go": 'package authparser\n\nimport "C"\n',
        })
        graph = lib.make(files, pkg)
        lib.node_by_output(graph, f"$(B)/{pkg}/decode.cgo2.c.o")


if __name__ == "__main__":
    unittest.main(verbosity=2)
