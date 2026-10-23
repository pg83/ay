import json
import hashlib
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


LLD_LDFLAGS = "IF (OS_LINUX)\n    LDFLAGS(-fuse-ld=lld --ld-path=${LLD_ROOT_RESOURCE_GLOBAL}/bin/ld.lld -Wl,--no-rosegment -Wl,--build-id=sha1)\nENDIF()\n"

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
        "build/conf/go.conf": "GOSTD_VERSION=1.26\n",
    }
    resources_library(files, "build/external_resources/go_tools", "GO_TOOLS")
    resources_library(files, "build/external_resources/yolint", "YOLINT")
    resources_library(files, "build/platform/lld", "LLD_ROOT", extra=LLD_LDFLAGS)
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
        lib.seed_root(root)
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
    def test_go_program_consumes_own_and_inherited_ld_plugins(self):
        files = go_world()
        go_module(files, "plugins/peer", macros="LD_PLUGIN(peer.py)\n")
        go_module(files, "app", srcs="app.go", module="GO_PROGRAM", macros="PEERDIR(plugins/peer)\nLD_PLUGIN(own.py)\n")
        files["plugins/peer/peer.py"] = "def plugin(): pass\n"
        files["app/own.py"] = "def plugin(): pass\n"
        graph = lib.make(files, "app")
        node = lib.node_by_output(graph, "$(B)/app/app")
        plugins = ["$(B)/plugins/peer/peer.py.pyplugin", "$(B)/app/own.py.pyplugin"]
        self.assertEqual(between(go_tool_args(node), "++ld_plugins", "++vcs"), plugins)
        for output in plugins:
            producer = lib.node_by_output(graph, output)
            self.assertIn(output, node["inputs"])
            self.assertIn(producer["uid"], node["deps"])

    def test_go_runtime_opt_ins_follow_statement_order(self):
        for macros, expected in (("", ()), ("USE_CXX()\n", ("cxxsupp",)),
                                 ("USE_UTIL()\n", ("cxxsupp", "util")),
                                 ("USE_UTIL()\nNO_RUNTIME()\n", ())):
            with self.subTest(macros=macros):
                files = go_world()
                for directory in ("contrib/libs/cxxsupp", "util"):
                    files[f"{directory}/ya.make"] = f"LIBRARY()\n{NO_PLATFORM}SRCS(runtime.cpp)\nEND()\n"
                    files[f"{directory}/runtime.cpp"] = "int runtime;\n"
                go_module(files, "library/go/example", macros=macros)
                graph = lib.make(files, "library/go/example")
                peers = go_node(graph, "library/go/example")["inputs"]
                self.assertEqual(any("cxxsupp" in peer for peer in peers), "cxxsupp" in expected)
                self.assertEqual("$(B)/util/libutil.a" in peers, "util" in expected)

    def test_embed_directory_modes_include_only_the_requested_tree(self):
        for all_files in (False, True):
            with self.subTest(all_files=all_files):
                files = go_world()
                go_module(files, "embed/sample", macros=f"GO_EMBED_DIR({'ALL ' if all_files else ''}assets)\n")
                for name in ("visible.txt", "nested/data.json", ".hidden", "_private", "nested/.hidden", "_cache/data"):
                    files["embed/sample/assets/" + name] = name
                files["embed/sample/outside.txt"] = "outside"
                graph = lib.make(files, "embed/sample")
                node = go_node(graph, "embed/sample")
                args = go_tool_args(node)
                start = args.index("++embed")
                expected = ["nested/data.json", "visible.txt"]
                if all_files:
                    expected = [".hidden", "_cache/data", "_private", "nested/.hidden", *expected]
                inputs = ["$(S)/embed/sample/assets/" + name for name in expected]
                self.assertEqual(args[start:start + 3], [
                    "++embed", "$(S)/embed/sample", ("all:" if all_files else "") + "assets",
                ])
                self.assertCountEqual(args[start + 3:args.index("++peers")], inputs)
                self.assertTrue(set(inputs).issubset(node["inputs"]))

    def test_embed_directory_preserves_declared_parent_path(self):
        files = go_world()
        go_module(files, "embed/sample", macros="GO_EMBED_DIR(../assets)\n")
        files["embed/assets/a/deep/x.txt"] = "x"
        files["embed/assets/b/y.txt"] = "y"
        args = go_tool_args(go_node(lib.make(files, "embed/sample"), "embed/sample"))
        self.assertEqual(args[args.index("++embed"):args.index("++peers")], [
            "++embed", "$(S)/embed/sample", "../assets",
            "$(S)/embed/sample/../assets/b/y.txt",
            "$(S)/embed/sample/../assets/a/deep/x.txt",
        ])

    def test_embed_bindir_tracks_generated_files_and_explicit_prefix(self):
        files = go_world()
        lib.tool_program(files, "tools/generate", "generate")
        go_module(files, "embed/generated", macros=(
            "RUN_PROGRAM(tools/generate OUT_NOAUTO catalog.pb ${ARCADIA_BUILD_ROOT}/shared/data/other.pb)\n"
            "GO_EMBED_BINDIR(catalog.pb FILES catalog.pb)\n"
            "GO_EMBED_BINDIR(PREFIX shared/data other FILES other.pb)\n"
            "GO_TEST_EMBED_BINDIR(test FILES ignored.pb)\n"
        ))
        graph = lib.make(files, "embed/generated")
        node = go_node(graph, "embed/generated")
        args = go_tool_args(node)
        start = args.index("++embed")
        self.assertEqual(args[start:args.index("++peers")], [
            "++embed", "$(B)/embed/generated", "catalog.pb", "$(B)/embed/generated/catalog.pb",
            "++embed", "$(B)/shared/data", "other", "$(B)/shared/data/other.pb",
        ])
        generated = lib.node_by_output(graph, "$(B)/embed/generated/catalog.pb")
        self.assertIn(generated["uid"], node["deps"])
        self.assertIn("$(B)/embed/generated/catalog.pb", node["inputs"])
        self.assertIn("$(B)/shared/data/other.pb", node["inputs"])

    def test_invalid_embed_arguments_report_errors(self):
        for macro, message in (
            ("GO_EMBED_BINDIR(PREFIX)", "PREFIX requires a path"),
            ("GO_EMBED_BINDIR(first second)", "expects one pattern"),
            ("GO_EMBED_BINDIR()", "expects a pattern"),
            ("GO_EMBED_DIR()", "expects a directory"),
        ):
            with self.subTest(macro=macro):
                files = go_world()
                go_module(files, "lib/invalid", macros=macro + "\n")
                result = lib.make_process(files, "lib/invalid")
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(message, result.stderr)
                self.assertNotIn("panic:", result.stderr)

    def test_generated_go_sources_precede_epilogue_symabis_output(self):
        files = go_world()
        lib.tool_program(files, "tools/generate", "generate")
        go_module(files, "lib/assembly", srcs="assembly.go bits.s", macros=(
            "RUN_PROGRAM(tools/generate OUT generated.go)\n"
        ))
        files["lib/assembly/bits.s"] = '#include "textflag.h"\n'
        graph = lib.make(files, "lib/assembly")
        args = go_tool_args(go_node(graph, "lib/assembly"))
        self.assertEqual(between(args, "++srcs", "++asm-flags"), [
            "$(B)/lib/assembly/generated.go", "$(B)/lib/assembly/gen.symabis",
            "$(S)/lib/assembly/assembly.go", "$(S)/lib/assembly/bits.s",
        ])

    def test_renamed_transitive_go_archives_keep_their_language(self):
        files = go_world()
        go_module(files, "lib/leaf")
        files["lib/leaf/ya.make"] = "GO_LIBRARY(renamed)\nSRCS(leaf.go)\nEND()\n"
        go_module(files, "lib/middle", macros="PEERDIR(lib/leaf)\n")
        files["lib/native/ya.make"] = f"LIBRARY()\n{NO_PLATFORM}SRCS(native.cpp)\nEND()\n"
        files["lib/native/native.cpp"] = "int native;\n"
        go_module(files, "cmd/app", macros="PEERDIR(lib/middle lib/native)\n", module="GO_PROGRAM")
        graph = lib.make(files, "cmd/app")
        args = go_tool_args(lib.node_by_output(graph, "$(B)/cmd/app/app"))
        self.assertEqual(between(args, "++non-local-peers", "++cgo-peers"), ["lib/leaf/renamed.a"])
        self.assertEqual(between(args, "++cgo-peers", "--ya-end-command-file"), [
            "cmd/app/__vcs_version__.c.o", "lib/native/liblib-native.a",
        ])

    def test_raw_string_imports_become_peers(self):
        files = go_world()
        go_module(files, f"{STD}/fmt")
        go_module(files, "vendor/example.com/rt")
        go_module(files, "library/go/raw", 'import (\n    `fmt`\n    rt `example.com/rt`\n)\n')
        graph = lib.make(files, "library/go/raw")
        raw = lib.node_by_output(graph, "$(B)/library/go/raw/raw.a")
        self.assertEqual(between(go_tool_args(raw), "++peers", "--ya-end-command-file"), [
            f"{STD}/fmt/fmt.a", "vendor/example.com/rt/rt.a",
        ])

    def test_resource_in_a_go_module_runs_the_resource_compiler(self):
        files = go_world()
        go_module(files, "library/go/core/resource")
        go_module(files, "library/go/core/resource/cc", module="GO_PROGRAM")
        files["certs/cacert.pem"] = "pem\n"
        go_module(files, "library/go/certs", macros="RESOURCE(\n    certs/cacert.pem /certifi/common.pem\n)\n")
        graph = lib.make(files, "library/go/certs")
        out = "$(B)/library/go/certs/resource.5e5de0f6d001776f76a82744c889271f.res.go"
        rc = lib.node_by_output(graph, out)
        self.assertEqual(rc["kv"]["p"], "PR")
        self.assertEqual(rc["cmds"][0]["cmd_args"], [
            "$(B)/library/go/core/resource/cc/cc", "-package", "certs", "-o", out,
            "$(S)/certs/cacert.pem", "notafile/certifi/common.pem",
        ])
        package = go_tool_args(go_node(graph, "library/go/certs"))
        self.assertEqual(between(package, "++srcs", "++asm-flags"), [out, "$(S)/library/go/certs/certs.go"])
        self.assertIn("library/go/core/resource/resource.a", between(package, "++peers", "--ya-end-command-file"))
        self.assertFalse([o for n in graph["graph"] for o in n["outputs"] if o.endswith(".global.a")])

    def test_go_resources_resolve_generated_inputs_after_their_producers(self):
        files = go_world()
        go_module(files, "library/go/core/resource")
        go_module(files, "library/go/core/resource/cc", module="GO_PROGRAM")
        lib.tool_program(files, "tools/generate", "generate")
        go_module(files, "lib/assets", macros=(
            "RESOURCE(lib/assets/schema.json schema.json)\n"
            "RUN_PROGRAM(tools/generate OUT_NOAUTO schema.json)\n"
        ))
        graph = lib.make(files, "lib/assets")
        producer = lib.node_by_output(graph, "$(B)/lib/assets/schema.json")
        resource = next(n for n in graph["graph"] if any(o.endswith(".res.go") for o in n["outputs"]))
        self.assertIn("$(B)/lib/assets/schema.json", resource["inputs"])
        self.assertIn("$(B)/lib/assets/schema.json", resource["cmds"][0]["cmd_args"])
        self.assertNotIn("$(S)/lib/assets/schema.json", resource["inputs"])
        self.assertIn(producer["uid"], resource["deps"])

    def test_resource_package_name_uses_module_name_or_test_import_path(self):
        for macro, package in (("", "custom"), ("SET(GO_TEST_IMPORT_PATH other/tested)", "tested")):
            with self.subTest(macro=macro):
                files = go_world()
                go_module(files, "library/go/core/resource")
                go_module(files, "library/go/core/resource/cc", module="GO_PROGRAM")
                files["lib/assets/ya.make"] = f"GO_LIBRARY(custom)\n{macro}\nRESOURCE(data.txt key)\nEND()\n"
                files["lib/assets/data.txt"] = "data\n"
                graph = lib.make(files, "lib/assets")
                resource = next(n for n in graph["graph"] if any(p.endswith(".res.go") for p in n["outputs"]))
                args = resource["cmds"][0]["cmd_args"]
                self.assertEqual(args[args.index("-package") + 1], package)
                self.assertIn("$(S)/lib/assets/data.txt", resource["inputs"])

    def test_resource_files_dispatch_to_the_go_resource_compiler(self):
        for macro, filename in (
            ("RESOURCE_FILES(PREFIX assets/ STRIP data/ data/a.txt)", "data/a.txt"),
            ("ALL_RESOURCE_FILES(txt PREFIX assets/ STRIP data/ data)", "${ARCADIA_ROOT}/p/data/a.txt"),
        ):
            with self.subTest(macro=macro):
                files = go_world()
                go_module(files, "library/go/core/resource")
                go_module(files, "library/go/core/resource/cc", module="GO_PROGRAM")
                go_module(files, "p", macros=macro + "\n")
                files["p/data/a.txt"] = "data\n"
                graph = lib.make(files, "p")
                key = "resfs/file/assets/a.txt"
                raw = ["-", 'resfs/src/' + key + '=${rootrel;context=TEXT;input=TEXT:"' + filename + '"}', filename, key]
                digest = hashlib.md5("@".join(raw).encode()).hexdigest()
                out = f"$(B)/p/resource.{digest}.res.go"
                resource = lib.node_by_output(graph, out)
                self.assertEqual(resource["cmds"][0]["cmd_args"][-4:], [
                    "-", "notafileresfs/src/resfs/file/assets/a.txt=p/data/a.txt",
                    "$(S)/p/data/a.txt", "notafileresfs/file/assets/a.txt",
                ])
                self.assertIn("$(S)/p/data/a.txt", resource["inputs"])
                self.assertIn(out, go_tool_args(go_node(graph, "p")))
                self.assertFalse(any(n["kv"]["p"] == "RC" for n in graph["graph"]))

    def test_embed_patterns_list_their_glob_matches(self):
        files = go_world()
        pkg = "library/go/embedded"
        go_module(files, pkg, macros="GO_EMBED_PATTERN(templates)\nGO_EMBED_PATTERN(*.sql)\nGO_EMBED_PATTERN(one.txt)\n")
        for name in ("templates/b.tmpl", "templates/a/z.tmpl", "templates/a/deep/y.tmpl", "templates/c/x.tmpl",
                     "templates/_skip.tmpl", "templates/.hidden", "templates/_dir/w.tmpl",
                     "2.sql", "1.sql", "one.txt"):
            files[f"{pkg}/{name}"] = "x\n"
        graph = lib.make(files, pkg)
        package = go_node(graph, pkg)
        args = go_tool_args(package)
        s = f"$(S)/{pkg}"
        self.assertEqual(between(args, "++cgo-srcs", "++peers"), [
            "++embed", s, "templates",
            f"{s}/templates/a/z.tmpl", f"{s}/templates/c/x.tmpl", f"{s}/templates/a/deep/y.tmpl", f"{s}/templates/b.tmpl",
            "++embed", s, "*.sql", f"{s}/1.sql", f"{s}/2.sql",
            "++embed", s, "one.txt", f"{s}/one.txt",
        ])
        self.assertIn(f"{s}/templates/a/deep/y.tmpl", package["inputs"])
        self.assertIn(f"{s}/one.txt", package["inputs"])

    def test_use_sdc_meta_merges_sdcmeta_after_the_go_link(self):
        files = go_world()
        go_module(files, "cmd/sdc", macros="USE_SDC_META()\n", module="GO_PROGRAM")
        graph = lib.make(files, "cmd/sdc")
        link = lib.node_by_output(graph, "$(B)/cmd/sdc/sdc")
        self.assertEqual(link["outputs"], ["$(B)/cmd/sdc/sdc", "$(B)/cmd/sdc/sdc.vet.txt", "$(B)/cmd/sdc/sdc.sdcmeta"])
        self.assertEqual(link["cmds"][-1]["cmd_args"][1:], [
            "$(S)/build/internal/scripts/merge_sdcmeta.py", "$(B)", "$(B)/cmd/sdc/sdc.sdcmeta",
        ])
        self.assertIn("$(S)/build/internal/scripts/merge_sdcmeta.py", link["inputs"])

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
            f'$(B)/{STD}/internal/fmtsort/fmtsort.a',
            f'$(B)/{STD}/vendor/golang.org/x/text/width/width.a',
            f'$(B)/{STD}/fmt/fmt.a',
            '$(B)/vendor/github.com/pkg/errors/errors.a',
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
            ("compressed-debug", {"build/ymake_conf.py": lib.ymake_conf(compress_debug=True)}, (), [
                "--target=aarch64-linux-gnu", "-B/usr/bin",
                "-Wl,--whole-archive", "-Wl,--no-whole-archive", "--cgo-peers",
                "-Wl,--compress-debug-sections=zstd", "-ldl", "-lrt", "-Wl,--no-as-needed",
                "-Wl,--gdb-index", "-fuse-ld=lld", "--ld-path=$(B)/resources/LLD_ROOT/bin/ld.lld",
                "-Wl,--no-rosegment", "-Wl,--build-id=sha1", "-lpthread", "-ldl", "-lresolv",
                "-nodefaultlibs", "-lpthread", "-lc", "-lm",
            ]),
            ("ld-conf-system-libraries", {"build/conf/linkers/ld.conf": lib.FAKE_LD_CONF + (
                'when ($OS_LINUX == "yes" && $MUSL != "yes") {\n'
                "    _C_SYSTEM_LIBRARIES=$_LD_USE_STDLIB -Wl,-lpthread,-lc\n}\n"
                'when ($OS_ANDROID == "yes") {\n    _C_SYSTEM_LIBRARIES=$_LD_USE_STDLIB -Wl,-lc\n}\n'
            )}, (), [
                "--target=aarch64-linux-gnu", "-B/usr/bin",
                "-Wl,--whole-archive", "-Wl,--no-whole-archive", "--cgo-peers",
                "-ldl", "-lrt", "-Wl,--no-as-needed",
                "-Wl,--gdb-index", "-fuse-ld=lld", "--ld-path=$(B)/resources/LLD_ROOT/bin/ld.lld",
                "-Wl,--no-rosegment", "-Wl,--build-id=sha1", "-lpthread", "-ldl", "-lresolv",
                "-nodefaultlibs", "-Wl,-lpthread,-lc", "-lm",
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

    def test_go_link_takes_peer_ldflags_extralibs_and_global_cflags(self):
        files = go_world()
        files["c/ya.make"] = f"LIBRARY()\n{NO_PLATFORM}CFLAGS(GLOBAL -DFROM_PEER)\nEXTRALIBS(-lrt -ldl)\nSRCS(c.cpp)\nEND()\n"
        files["c/c.cpp"] = "int c;\n"
        go_module(files, "cmd/app", macros="PEERDIR(c)\n", module="GO_PROGRAM")
        graph = lib.make(files, "cmd/app")
        link = lib.node_by_output(graph, "$(B)/cmd/app/app")
        extld = between(go_tool_args(link), "++extldflags", "++peers")
        self.assertEqual(extld[extld.index("-Wl,--build-id=sha1") + 1:extld.index("-lresolv") + 1], [
            "-lrt", "-ldl", "-lpthread", "-ldl", "-lresolv",
        ])
        self.assertIn("-DFROM_PEER", link["cmds"][1]["cmd_args"])

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
            f"{STD}/bufio/bufio.a", f"{STD}/hash/hash.a", f"{STD}/context/context.a",
        ])
        context = lib.node_by_output(graph, f"$(B)/{STD}/context/context.a")
        self.assertIn(context["uid"], go_node(graph, "library/go/mixed")["deps"])
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
            *PROGRAM_PEERS, "library/go/empty/empty.a", "vendor/github.com/x/nocgo/nocgo.a",
        ])

    def test_autocheck_generates_no_sbom_components(self):
        files = go_world()
        files["build/internal/conf/sbom.conf"] = "# sbom\n"
        go_module(files, "vendor/github.com/pkg/errors", macros="LICENSE(BSD-2-Clause)\nVERSION(v0.9.1)\n")
        go_module(files, "cmd/app", 'import "github.com/pkg/errors"\n', module="GO_PROGRAM")
        graph = lib.make(files, "cmd/app", "--target-platform", "default-linux-x86_64", "--release", "-DAUTOCHECK=yes")
        self.assertEqual([o for n in graph["graph"] for o in n["outputs"] if o.endswith(".sbom")], [])

    def test_release_program_embeds_go_sbom_components(self):
        files = go_world()
        files["build/internal/conf/sbom.conf"] = "# sbom\n"
        files["build/platform/python/ymake_python3/ya.make"] = (
            "RESOURCES_LIBRARY()\nTOOLCHAIN(python3)\nVERSION(3.12.6)\nEND()\n"
        )
        resources_library(files, "build/platform/lld", "LLD_ROOT", extra="TOOLCHAIN(lld)\nVERSION(20)\n" + LLD_LDFLAGS)
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
        # The packages take the components, not the script that made them:
        # no GO node of the devtools_yamaker reference lists gen_sbom.py.
        errors = go_node(graph, "vendor/github.com/pkg/errors")
        self.assertEqual(errors["inputs"][-2:], [python_sbom, errors_sbom])
        self.assertNotIn("$(S)/build/internal/scripts/gen_sbom.py", errors["inputs"])
        self.assertIn(component["uid"], errors["deps"])
        wrap = go_node(graph, "library/go/wrap")
        self.assertEqual(wrap["inputs"][-2:], [python_sbom, errors_sbom])
        self.assertNotIn("$(S)/build/internal/scripts/gen_sbom.py", wrap["inputs"])

        link = lib.node_by_output(graph, "$(B)/cmd/app/app")
        self.assertEqual(len(link["cmds"]), 6)
        sbom_cmd, objcopy = link["cmds"][3]["cmd_args"], link["cmds"][5]["cmd_args"]
        self.assertEqual(sbom_cmd[1:], [
            "$(S)/build/internal/scripts/link_sbom.py", "--lang", "GO", "--mod-path", "cmd/app",
            "--output", "$(B)/cmd/app/__sbomdata.json", "--vcs-info", "$(B)/vcs.json",
            python_sbom, "$(B)/build/platform/lld/toolchain.component.sbom", errors_sbom,
        ])
        self.assertEqual(link["cmds"][3]["cwd"], "$(B)")
        self.assertEqual(objcopy[1:], [
            "--add-section",
            ".rosbomdata=$(B)/cmd/app/__sbomdata.json",
            "$(B)/cmd/app/app",
        ])
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
