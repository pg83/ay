import json
import os
import tempfile
import unittest
from pathlib import Path

import lib


STD = "contrib/go/_std_1.26/src"
NO_PLATFORM = "NO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
TOOL_SCRIPTS = [
    "$(S)/build/scripts/go_tool.py",
    "$(S)/build/scripts/process_command_files.py",
    "$(S)/build/scripts/process_whole_archive_option.py",
    "$(S)/build/rules/go/migrations.yaml",
    "$(S)/build/rules/go/extended_lint.yaml",
    "$(S)/build/rules/go/risky_imports.yaml",
]
PROGRAM_PEERS = [
    f"{STD}/runtime/cgo/cgo.a",
    f"{STD}/runtime/runtime.a",
    "library/go/core/buildinfo/buildinfo.a",
]


def resources_library(files, path, name, extra=""):
    bundle = f"{name.lower()}.json"
    files[f"{path}/ya.make"] = (
        f"RESOURCES_LIBRARY()\n{extra}"
        f"DECLARE_EXTERNAL_HOST_RESOURCES_BUNDLE_BY_JSON({name} {bundle})\nEND()\n"
    )
    files[f"{path}/{bundle}"] = json.dumps({"by_platform": {
        platform: {"uri": f"sbr:{len(files)}{index}"}
        for index, platform in enumerate(("linux", "linux-x86_64", "linux-aarch64"))
    }})


def go_module(files, path, body="", srcs=None, macros="", module="GO_LIBRARY"):
    name = path.rsplit("/", 1)[-1]
    files[f"{path}/ya.make"] = (
        f"{module}()\n{macros}SRCS({srcs if srcs is not None else name + '.go'})\nEND()\n"
    )
    files[f"{path}/{name}.go"] = f"package {name}\n{body}"


def go_world():
    files = {
        "build/cow/on/ya.make": f"LIBRARY()\n{NO_PLATFORM}END()\n",
        "contrib/libs/linux-headers/ya.make": f"LIBRARY()\n{NO_PLATFORM}END()\n",
        "build/scripts/go_fake_include/go_asm.h": "// written by the go tool\n",
        f"{STD}/runtime/textflag.h": "#define NOSPLIT 4\n",
    }
    resources_library(files, "build/external_resources/go_tools", "GO_TOOLS")
    resources_library(files, "build/external_resources/yolint", "YOLINT")
    resources_library(files, "build/platform/lld", "LLD_ROOT")
    for path in (f"{STD}/runtime", f"{STD}/runtime/cgo", "library/go/core/buildinfo"):
        go_module(files, path)
    return files


def go_tool_args(node):
    for cmd in node["cmds"]:
        if "$(S)/build/scripts/go_tool.py" in cmd["cmd_args"]:
            return cmd["cmd_args"]
    raise AssertionError(f"{node['outputs']} has no go_tool.py command")


def between(args, start, end):
    begin = args.index(start) + 1
    return args[begin:args.index(end, begin)]


def make_with_env(files, target, extra_env):
    with tempfile.TemporaryDirectory(prefix="ay-go-test-") as directory:
        root = Path(directory)
        (root / ".arcadia.root").touch()
        (root / "ya.conf").write_text('[flags]\nOPENSOURCE = "yes"\n\n[host_platform_flags]\nOPENSOURCE = "yes"\n')
        for relative, content in files.items():
            path = root / relative
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(content)
        env = {key: value for key, value in os.environ.items() if key not in lib.TOOLCHAIN_ENV_VARS}
        return lib.run(
            "make", "-j0", "-G", "--sandboxing", "--source-root", root,
            "--target-platform", "default-linux-aarch64", "--host-platform", "default-linux-x86_64",
            target, env={**env, **extra_env},
        )


def go_node(graph, path):
    name = path.rsplit("/", 1)[-1]
    return lib.node_by_output(graph, f"$(B)/{path}/{name}.a")


class GoPackageGraphTest(unittest.TestCase):
    def test_program_link_partitions_direct_transitive_and_cxx_archives(self):
        files = go_world()
        go_module(files, f"{STD}/fmt", 'import (\n\t"internal/fmtsort"\n\t"golang.org/x/text/width"\n)\n')
        go_module(files, f"{STD}/internal/fmtsort")
        go_module(files, f"{STD}/vendor/golang.org/x/text/width")
        go_module(files, "vendor/github.com/pkg/errors")
        go_module(files, "library/go/greet", 'import (\n\t"fmt"\n\n\t"github.com/pkg/errors"\n)\n')
        files["contrib/libs/cxxlib/ya.make"] = f"LIBRARY()\n{NO_PLATFORM}SRCS(lib.cpp)\nEND()\n"
        files["contrib/libs/cxxlib/lib.cpp"] = "int f() { return 0; }\n"
        go_module(
            files, "cmd/app",
            'import (\n\t"fmt"\n\t"unsafe"\n\n\t"a.yandex-team.ru/library/go/greet"\n\t"example.com/absent"\n)\n',
            macros="PEERDIR(contrib/libs/cxxlib)\n", module="GO_PROGRAM",
        )
        graph = lib.make(files, "cmd/app")

        fmt = go_tool_args(go_node(graph, f"{STD}/fmt"))
        self.assertEqual(between(fmt, "++peers", "--ya-end-command-file"), [
            f"{STD}/internal/fmtsort/fmtsort.a",
            f"{STD}/vendor/golang.org/x/text/width/width.a",
        ])
        greet = go_node(graph, "library/go/greet")
        self.assertEqual(between(go_tool_args(greet), "++peers", "--ya-end-command-file"), [
            f"{STD}/fmt/fmt.a",
            "vendor/github.com/pkg/errors/errors.a",
        ])
        self.assertEqual(greet["outputs"], [
            "$(B)/library/go/greet/greet.a",
            "$(B)/library/go/greet/greet.a.vet.out",
            "$(B)/library/go/greet/greet.a.vet.txt",
        ])
        self.assertEqual(greet["kv"]["p"], "GO")
        self.assertEqual(greet["inputs"][:7], ["$(S)/library/go/greet/greet.go", *TOOL_SCRIPTS])
        self.assertEqual(greet["inputs"][-4:], [
            f"$(S)/{STD}/internal/fmtsort/fmtsort.go",
            f"$(S)/{STD}/vendor/golang.org/x/text/width/width.go",
            f"$(S)/{STD}/fmt/fmt.go",
            "$(S)/vendor/github.com/pkg/errors/errors.go",
        ])
        self.assertEqual(greet["env"]["GOOS"], "linux")
        self.assertEqual(greet["env"]["GOARCH"], "amd64")

        link = lib.node_by_output(graph, "$(B)/cmd/app/app")
        self.assertEqual(link["kv"]["p"], "LD")
        self.assertEqual(link["outputs"], ["$(B)/cmd/app/app", "$(B)/cmd/app/app.vet.txt"])
        vcs_c, vcs_o, vcs_go, go_link = link["cmds"]
        self.assertEqual(vcs_c["cmd_args"][1:4], [
            "$(S)/build/scripts/vcs_info.py", "$(B)/vcs.json", "$(B)/cmd/app/__vcs_version__.c",
        ])
        self.assertIn("$(B)/cmd/app/__vcs_version__.c.o", vcs_o["cmd_args"])
        self.assertEqual(vcs_go["cmd_args"][1:], [
            "$(S)/build/scripts/vcs_info.py", "output-go", "$(B)/vcs.json",
            "$(B)/cmd/app/__vcs_version__.go", "a.yandex-team.ru/",
        ])
        args = go_link["cmd_args"]
        self.assertEqual(between(args, "++mode", "++std-lib-prefix"), ["exe"])
        self.assertEqual(between(args, "++srcs", "++asm-flags"), ["$(S)/cmd/app/app.go"])
        self.assertEqual(between(args, "++vcs", "++extld"), ["$(B)/cmd/app/__vcs_version__.go"])
        self.assertEqual(between(args, "++peers", "++non-local-peers"), [
            *PROGRAM_PEERS, f"{STD}/fmt/fmt.a", "library/go/greet/greet.a",
        ])
        self.assertEqual(between(args, "++non-local-peers", "++cgo-peers"), [
            f"{STD}/internal/fmtsort/fmtsort.a",
            f"{STD}/vendor/golang.org/x/text/width/width.a",
            "vendor/github.com/pkg/errors/errors.a",
        ])
        self.assertEqual(between(args, "++cgo-peers", "--ya-end-command-file"), [
            "cmd/app/__vcs_version__.c.o",
            "contrib/libs/cxxlib/libcontrib-libs-cxxlib.a",
        ])
        self.assertIn("$(B)/contrib/libs/cxxlib/libcontrib-libs-cxxlib.a", link["inputs"])
        self.assertEqual(link["inputs"][-2:], [
            "$(S)/build/scripts/c_templates/svn_interface.c",
            "$(S)/build/scripts/c_templates/svnversion.h",
        ])

    def test_assembly_sources_get_symabis_node_with_package_import_path(self):
        files = go_world()
        for path, package in (
            (f"{STD}/internal/bytealg", "internal/bytealg"),
            ("vendor/github.com/klauspost/cpuid", "github.com/klauspost/cpuid"),
            ("library/go/simd", "a.yandex-team.ru/library/go/simd"),
        ):
            with self.subTest(path=path):
                name = path.rsplit("/", 1)[-1]
                go_module(files, path, srcs=f"{name}.go {name}_amd64.s")
                files[f"{path}/{name}_amd64.s"] = (
                    '#include "go_asm.h"\n#include "textflag.h"\nTEXT ·f(SB),NOSPLIT,$0\n'
                )
                graph = lib.make(files, path)
                symabis = lib.node_by_output(graph, f"$(B)/{path}/gen.symabis")
                self.assertEqual(symabis["kv"]["p"], "go")
                # The source comes first; the order of its include closure
                # follows closure buckets, which depend on path ids.
                self.assertEqual(symabis["inputs"][0], f"$(S)/{path}/{name}_amd64.s")
                self.assertCountEqual(symabis["inputs"][1:], [
                    "$(S)/build/scripts/go_fake_include/go_asm.h",
                    f"$(S)/{STD}/runtime/textflag.h",
                ])
                self.assertEqual(symabis["cmds"][0]["cmd_args"], [
                    "$(B)/resources/GO_TOOLS/pkg/tool/linux_amd64/asm",
                    "-trimpath", "$(S)=>/-S;$(B)=>/-B;$(TOOL_ROOT)=>/-T",
                    "-I", "$(B)", "-I", "$(S)",
                    "-I", f"$(S)/{STD}/runtime",
                    "-I", "$(S)/build/scripts/go_fake_include",
                    "-I", "$(B)/resources/GO_TOOLS/pkg/include",
                    "-D", "GOOS_linux", "-D", "GOARCH_amd64",
                    "-p", package, "-gensymabis",
                    "-o", f"$(B)/{path}/gen.symabis",
                    f"$(S)/{path}/{name}_amd64.s",
                ])
                package_node = go_node(graph, path)
                self.assertEqual(between(go_tool_args(package_node), "++srcs", "++asm-flags"), [
                    f"$(B)/{path}/gen.symabis",
                    f"$(S)/{path}/{name}.go",
                    f"$(S)/{path}/{name}_amd64.s",
                ])
                self.assertEqual(package_node["deps"][0], symabis["uid"])
                self.assertCountEqual(package_node["inputs"][-2:], [
                    "$(S)/build/scripts/go_fake_include/go_asm.h",
                    f"$(S)/{STD}/runtime/textflag.h",
                ])

    def test_host_tool_program_is_built_position_independent(self):
        files = go_world()
        go_module(files, "vendor/github.com/klauspost/cpuid", srcs="cpuid.go cpuid_amd64.s")
        files["vendor/github.com/klauspost/cpuid/cpuid_amd64.s"] = (
            '#include "textflag.h"\nTEXT ·asmCpuid(SB),NOSPLIT,$0\n'
        )
        go_module(files, "tools/gogen", 'import "github.com/klauspost/cpuid"\n', module="GO_PROGRAM")
        files["lib/gen/ya.make"] = (
            f"LIBRARY()\n{NO_PLATFORM}RUN_PROGRAM(tools/gogen out.cpp OUT out.cpp)\nEND()\n"
        )
        graph = lib.make(files, "lib/gen")

        symabis = lib.node_by_output(graph, "$(B)/vendor/github.com/klauspost/cpuid/gen.symabis")
        self.assertEqual(
            between(symabis["cmds"][0]["cmd_args"], "-p", "-o"),
            ["github.com/klauspost/cpuid", "-shared", "-gensymabis"],
        )
        package = go_tool_args(go_node(graph, "vendor/github.com/klauspost/cpuid"))
        self.assertEqual(between(package, "++asm-flags", "++link-flags"), ["-shared", "++compile-flags", "-shared"])

        link = go_tool_args(lib.node_by_output(graph, "$(B)/tools/gogen/gogen"))
        self.assertEqual(between(link, "++asm-flags", "++link-flags"), ["-shared", "++compile-flags", "-shared"])
        self.assertEqual(between(link, "++extldflags", "++peers"), [
            "--target=x86_64-linux-gnu", "-B/usr/bin",
            "-Wl,--whole-archive", "-Wl,--no-whole-archive", "--cgo-peers",
            "-ldl", "-lrt", "-Wl,--no-as-needed", "-fPIC", "-Wl,--gdb-index", "-fPIC",
            "-fuse-ld=lld", "--ld-path=$(B)/resources/LLD_ROOT/bin/ld.lld",
            "-Wl,--no-rosegment", "-Wl,--build-id=sha1", "-lpthread", "-ldl", "-lresolv",
            "-nodefaultlibs", "-lpthread", "-lc", "-lm",
        ])
        self.assertEqual(between(link, "++cgo-peers", "--ya-end-command-file"), [
            "tools/gogen/__vcs_version__.c.pic.o",
        ])

    def test_extld_flags_follow_platform_configuration(self):
        base = go_world()
        go_module(base, "cmd/app", module="GO_PROGRAM")
        cases = (
            ("compressed-debug", {"build/ymake_conf.py": "debug_info_flags.append('-gz=zstd')\n"}, (), [
                "--target=aarch64-linux-gnu", "-B/usr/bin",
                "-Wl,--whole-archive", "-Wl,--no-whole-archive", "--cgo-peers",
                "-Wl,--compress-debug-sections=zstd", "-ldl", "-lrt", "-Wl,--no-as-needed",
                "-Wl,--gdb-index", "-fuse-ld=lld", "--ld-path=$(B)/resources/LLD_ROOT/bin/ld.lld",
                "-Wl,--no-rosegment", "-Wl,--build-id=sha1", "-lpthread", "-ldl", "-lresolv",
                "-nodefaultlibs", "-lpthread", "-lc", "-lm",
            ]),
            ("musl-arcadia-libm", {}, ("--musl", "-DUSE_ARCADIA_LIBM=yes"), [
                "--target=aarch64-linux-gnu", "-B/usr/bin",
                "-Wl,--whole-archive", "-Wl,--no-whole-archive", "--cgo-peers",
                "-Wl,--no-as-needed", "-Wl,--gdb-index",
                "-fuse-ld=lld", "--ld-path=$(B)/resources/LLD_ROOT/bin/ld.lld",
                "-Wl,--no-rosegment", "-Wl,--build-id=sha1", "-lpthread", "-ldl", "-lresolv",
                "-nostdlib",
            ]),
        )
        for name, extra_files, flags, expected in cases:
            with self.subTest(name=name):
                graph = lib.make({**base, **extra_files}, "cmd/app", *flags)
                link = go_tool_args(lib.node_by_output(graph, "$(B)/cmd/app/app"))
                self.assertEqual(between(link, "++extldflags", "++peers"), expected)

    def test_import_declarations_become_implicit_peerdirs(self):
        files = go_world()
        imported = ("bufio", "bytes", "context", "errors", "flag", "hash", "io", "sort", "strconv", "gopkg.in/yaml.v2")
        ignored = ("log", "math", "net")
        for name in (*imported, *ignored):
            go_module(files, f"{STD}/{name}")
        sources = {
            "main.go": (
                "// Copyright header.\n/* Licensed under\n   the Apache License. */\n"
                "package main // trailing comment\n\n"
                'import "bufio"\n'
                "import (\n"
                '\tbts "bytes"\n'
                '\t. "context"\n'
                '\t_ "errors"\n'
                '\t// "log"\n'
                '\t/* "math" */\n'
                '\t"flag"; "hash"\n'
                '\tv2 "gopkg.in/yaml.v2"\n'
                ")\n"
                'import "unsafe"\n\n'
                'func main() {}\n\nimport "net"\n'
            ),
            "semicolon.go": 'package main\nimport "sort";\n',
            "open_comment.go": 'package main\nimport "strconv"\n/* never closed\n',
            "open_block.go": 'package main\nimport (\n\t"io"\n',
        }
        files.update({f"cmd/app/{name}": text for name, text in sources.items()})
        files["cmd/app/ya.make"] = f"GO_PROGRAM()\nSRCS({' '.join(sources)})\nEND()\n"
        graph = lib.make(files, "cmd/app")
        link = go_tool_args(lib.node_by_output(graph, "$(B)/cmd/app/app"))
        self.assertEqual(between(link, "++peers", "++non-local-peers"), [
            *PROGRAM_PEERS,
            *(f"{STD}/{name}/{name.rsplit('/', 1)[-1]}.a" for name in (
                "bufio", "bytes", "context", "errors", "flag", "hash", "gopkg.in/yaml.v2", "sort", "strconv", "io",
            )),
        ])

    def test_generated_absolute_global_and_simd_sources(self):
        files = go_world()
        for name in ("bufio", "bytes", "context", "hash"):
            go_module(files, f"{STD}/{name}")
        files.update({
            "library/go/mixed/ya.make": (
                "GO_LIBRARY()\nCOPY_FILE(template.txt gen.go)\n"
                "SRCS(mixed.go ${ARCADIA_ROOT}/library/go/shared/shared.go ${BINDIR}/gen.go GLOBAL global.go)\n"
                "SRC_C_AVX2(simd.c)\nEND()\n"
            ),
            "library/go/mixed/mixed.go": 'package mixed\nimport (\n\t"bufio"\n\t"bufio"\n)\n',
            "library/go/mixed/template.txt": 'package mixed\nimport "bytes"\n',
            "library/go/mixed/global.go": 'package mixed\nimport "context"\n',
            "library/go/mixed/simd.c": "int simd(void) { return 0; }\n",
            "library/go/shared/shared.go": 'package mixed\nimport "hash"\n',
        })
        go_module(files, "cmd/app", 'import "a.yandex-team.ru/library/go/mixed"\n', module="GO_PROGRAM")
        graph = lib.make(files, "cmd/app")

        mixed = go_tool_args(go_node(graph, "library/go/mixed"))
        self.assertEqual(between(mixed, "++srcs", "++asm-flags"), [
            "$(B)/library/go/mixed/simd.c.avx2.o",
            "$(S)/library/go/mixed/mixed.go",
            "$(S)/library/go/shared/shared.go",
            "$(B)/library/go/mixed/gen.go",
            "$(S)/library/go/mixed/global.go",
        ])
        self.assertEqual(between(mixed, "++peers", "--ya-end-command-file"), [
            f"{STD}/bufio/bufio.a", f"{STD}/hash/hash.a",
        ])
        simd = lib.node_by_output(graph, "$(B)/library/go/mixed/simd.c.avx2.o")
        self.assertEqual(simd["cmds"][0]["cmd_args"][-5:], [
            "-mavx2", "-mfma", "-mbmi", "-mbmi2", "$(S)/library/go/mixed/simd.c",
        ])
        with self.assertRaises(AssertionError):
            lib.node_by_output(graph, "$(B)/library/go/mixed/simd.c")

    def test_modules_without_go_sources(self):
        files = go_world()
        files["library/go/empty/ya.make"] = "GO_LIBRARY()\nEND()\n"
        files["vendor/github.com/x/nocgo/ya.make"] = "GO_LIBRARY()\nSRCS(nocgo.go CGO_EXPORT impl.c)\nEND()\n"
        files["vendor/github.com/x/nocgo/nocgo.go"] = "package nocgo\n"
        files["vendor/github.com/x/nocgo/impl.c"] = '#include "impl.h"\nint impl(void) { return 0; }\n'
        files["vendor/github.com/x/nocgo/impl.h"] = "int impl(void);\n"
        files["cmd/nothing/ya.make"] = "GO_PROGRAM()\nPEERDIR(library/go/empty vendor/github.com/x/nocgo)\nEND()\n"
        graph = lib.make(files, "cmd/nothing")

        empty = go_node(graph, "library/go/empty")
        self.assertEqual(between(go_tool_args(empty), "++srcs", "++asm-flags"), [])
        self.assertEqual(empty["inputs"], TOOL_SCRIPTS)

        nocgo = go_node(graph, "vendor/github.com/x/nocgo")
        self.assertEqual(between(go_tool_args(nocgo), "++srcs", "++asm-flags"), [
            "$(B)/vendor/github.com/x/nocgo/impl.c.o",
            "$(S)/vendor/github.com/x/nocgo/nocgo.go",
        ])
        self.assertEqual(between(go_tool_args(nocgo), "++cgo-srcs", "++peers"), [])
        copy = lib.node_by_output(graph, "$(B)/vendor/github.com/x/nocgo/impl.c")
        self.assertEqual(copy["inputs"], [
            "$(S)/vendor/github.com/x/nocgo/impl.c", "$(S)/vendor/github.com/x/nocgo/impl.h",
        ])
        impl = lib.node_by_output(graph, "$(B)/vendor/github.com/x/nocgo/impl.c.o")
        self.assertIn("-I$(S)/vendor/github.com/x/nocgo", impl["cmds"][0]["cmd_args"])
        self.assertEqual(impl["cmds"][0]["cmd_args"][-4:], [
            "-w", "-pthread", "-fpic", "$(B)/vendor/github.com/x/nocgo/impl.c",
        ])

        link = go_tool_args(lib.node_by_output(graph, "$(B)/cmd/nothing/nothing"))
        self.assertEqual(between(link, "++srcs", "++asm-flags"), [])
        self.assertEqual(between(link, "++peers", "++non-local-peers"), [
            "library/go/empty/empty.a", "vendor/github.com/x/nocgo/nocgo.a", *PROGRAM_PEERS,
        ])

    def test_release_program_embeds_go_sbom_components(self):
        files = go_world()
        files["build/internal/conf/sbom.conf"] = "# sbom\n"
        files["build/platform/python/ymake_python3/ya.make"] = (
            "RESOURCES_LIBRARY()\nTOOLCHAIN(python3)\nVERSION(3.12.6)\nEND()\n"
        )
        resources_library(files, "build/platform/lld", "LLD_ROOT", extra="TOOLCHAIN(lld)\nVERSION(20)\n")
        go_module(files, "vendor/github.com/pkg/errors", macros="LICENSE(BSD-2-Clause)\nVERSION(v0.9.1)\n")
        go_module(files, "library/go/wrap", 'import "github.com/pkg/errors"\n')
        go_module(files, "cmd/app", 'import "a.yandex-team.ru/library/go/wrap"\n', module="GO_PROGRAM")
        graph = lib.make(files, "cmd/app", "--target-platform", "default-linux-x86_64", "--release")
        python_sbom = "$(B)/build/platform/python/ymake_python3/toolchain.component.sbom"
        errors_sbom = "$(B)/vendor/github.com/pkg/errors/errors.GO.component.sbom"

        component = lib.node_by_output(graph, errors_sbom)
        self.assertEqual(component["cmds"][0]["cmd_args"][1:], [
            "$(S)/build/internal/scripts/gen_sbom.py", "--output", errors_sbom,
            "--type", "library", "--path", "vendor/github.com/pkg/errors", "--ver", "v0.9.1", "--lang", "GO",
        ])
        errors = go_node(graph, "vendor/github.com/pkg/errors")
        self.assertEqual(errors["inputs"][-3:], ["$(S)/build/internal/scripts/gen_sbom.py", python_sbom, errors_sbom])
        self.assertIn(component["uid"], errors["deps"])
        wrap = go_node(graph, "library/go/wrap")
        self.assertEqual(wrap["inputs"][-3:], ["$(S)/build/internal/scripts/gen_sbom.py", python_sbom, errors_sbom])

        link = lib.node_by_output(graph, "$(B)/cmd/app/app")
        self.assertEqual(len(link["cmds"]), 6)
        sbom_cmd, objcopy = link["cmds"][3]["cmd_args"], link["cmds"][5]["cmd_args"]
        self.assertEqual(sbom_cmd[1:], [
            "$(S)/build/internal/scripts/link_sbom.py", "--lang", "GO", "--mod-path", "cmd/app",
            "--output", "$(B)/cmd/app/__sbomdata.json", "--vcs-info", "$(B)/vcs.json",
            python_sbom, "$(B)/build/platform/lld/toolchain.component.sbom", errors_sbom,
        ])
        self.assertEqual(link["cmds"][3]["cwd"], "$(B)")
        self.assertEqual(objcopy[1:], ["--add-section", ".rosbomdata=$(B)/cmd/app/__sbomdata.json", "$(B)/cmd/app/app"])
        self.assertEqual(link["inputs"][-4:], [
            python_sbom, "$(B)/build/platform/lld/toolchain.component.sbom", errors_sbom,
            "$(S)/build/internal/scripts/link_sbom.py",
        ])
        self.assertIn(component["uid"], link["deps"])

    def test_go_node_slices_are_arena_owned(self):
        files = go_world()
        go_module(files, "vendor/github.com/klauspost/cpuid", srcs="cpuid.go cpuid_amd64.s")
        files["vendor/github.com/klauspost/cpuid/cpuid_amd64.s"] = (
            '#include "textflag.h"\nTEXT ·asmCpuid(SB),NOSPLIT,$0\n'
        )
        go_module(files, "cmd/app", 'import "github.com/klauspost/cpuid"\n', module="GO_PROGRAM")
        result = make_with_env(files, "cmd/app", {"AY_DEBUG_OWNERSHIP": "1"})
        self.assertEqual(result.stderr, "ownership: 0 violating (field, site) pairs\n")
        graph = json.loads(result.stdout)
        self.assertEqual(go_node(graph, "vendor/github.com/klauspost/cpuid")["env"]["GOOS"], "linux")


if __name__ == "__main__":
    unittest.main(verbosity=2)
